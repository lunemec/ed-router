use crate::cache::{SystemPoint, SystemPointExt};
use rayon::prelude::*;
use rkyv::{Archived, Deserialize, Infallible};
use rstar::RTree;
use serde::{Deserialize as SerdeDeserialize, Serialize};
use std::collections::{BinaryHeap, HashMap};

struct NodeCache<'a> {
    points: &'a [Archived<SystemPoint>],
    cache: HashMap<usize, SystemPoint>,
}

impl<'a> NodeCache<'a> {
    fn new(points: &'a [Archived<SystemPoint>]) -> Self {
        NodeCache {
            points,
            cache: HashMap::new(),
        }
    }

    /// Gets a deserialized SystemPoint, using the cache if available.
    fn get(&mut self, idx: usize) -> SystemPoint {
        // The entry API is perfect here. It checks for the key and allows
        // inserting a value only if the key doesn't exist, all in one operation.
        self.cache
            .entry(idx)
            .or_insert_with(|| self.points[idx].deserialize(&mut Infallible).unwrap())
            .clone() // We clone so the cache can be reused.
    }
}

// --- Public API ---

#[derive(Debug, Clone, SerdeDeserialize)]
pub struct SearchParameters {
    pub start_idx: usize,
    pub end_idx: usize,
    pub jump_range: f64,
    pub fuel_capacity: f64,
    pub use_neutron: bool,
    pub exclude_white_dwarfs: bool,
    pub max_star_distance: f64,
    pub greediness: f64,
}

#[derive(Debug, Clone, Serialize)]
pub struct JumpResult {
    pub system_id: i64,
    pub system_idx: usize,
    pub jump_distance: f64,
    pub fuel_used: f64,
    pub fuel_remaining: f64,
    pub is_refuel_stop: bool,
    pub boost_type: BoostType,
    pub warnings: Vec<String>,
}

/// The main entry point for running a bi-directional A* search.
pub fn find_route(
    params: &SearchParameters,
    rtree: &RTree<SystemPoint>,
    points: &[Archived<SystemPoint>], // And the slice of all points
) -> Option<Vec<JumpResult>> {
    let start_idx = params.start_idx;
    let goal_idx = params.end_idx;

    let mut node_cache = NodeCache::new(points);
    let start_node = node_cache.get(start_idx);
    let goal_node = node_cache.get(goal_idx);

    // --- Forward Search Initialization ---
    let mut open_set = BinaryHeap::new();
    let mut g_score: HashMap<usize, f64> = HashMap::new();
    let mut fuel_at_node: HashMap<usize, f64> = HashMap::new();
    let mut came_from: HashMap<usize, (usize, JumpDetails)> = HashMap::new();

    g_score.insert(start_idx, 0.0);
    fuel_at_node.insert(start_idx, params.fuel_capacity);
    let start_h = heuristic(&start_node, &goal_node) / params.jump_range;
    open_set.push(State {
        cost: 0.0,
        position_idx: start_idx,
        fuel: params.fuel_capacity,
        f_score: start_h * params.greediness,
    });

    while let Some(State {
        cost: current_g,
        position_idx: current_idx,
        ..
    }) = open_set.pop()
    {
        // --- GOAL CHECK ---
        // If we've reached the end, reconstruct the path and return.
        if current_idx == goal_node.idx {
            return Some(reconstruct_path(
                current_idx,
                &came_from,
                start_idx,
                params.fuel_capacity,
                &mut node_cache,
            ));
        }

        let current_system: SystemPoint = node_cache.get(current_idx);
        let (is_scoopable_here, best_boost_here) = analyze_system_features(&current_system, params);

        let fuel_after_scoop = if is_scoopable_here {
            params.fuel_capacity
        } else {
            *fuel_at_node.get(&current_idx).unwrap_or(&0.0)
        };

        let neighbor_results = expand_neighbors(
            &current_system,
            current_g,
            fuel_after_scoop,
            best_boost_here,
            params,
            rtree,
            &goal_node,
        );

        // The final loop is now much simpler.
        for (neighbor_idx, tentative_g_score, fuel_after_jump, f_score, details) in neighbor_results
        {
            if tentative_g_score < *g_score.get(&neighbor_idx).unwrap_or(&f64::INFINITY) {
                came_from.insert(neighbor_idx, (current_idx, details));
                g_score.insert(neighbor_idx, tentative_g_score);
                fuel_at_node.insert(neighbor_idx, fuel_after_jump);

                open_set.push(State {
                    cost: tentative_g_score,
                    position_idx: neighbor_idx,
                    fuel: fuel_after_jump,
                    f_score, // Use the pre-calculated f_score
                });
            }
        }
    }

    None
}

/// High-level function to find and evaluate all valid neighbors from a given system.
fn expand_neighbors(
    current_system: &SystemPoint,
    current_g: f64,
    fuel_after_scoop: f64,
    best_boost_here: BoostType,
    params: &SearchParameters,
    rtree: &RTree<SystemPoint>,
    goal_node: &SystemPoint,
) -> Vec<(usize, f64, f64, f64, JumpDetails)> {
    let mut possible_jumps = vec![(params.jump_range, BoostType::None)];
    if params.use_neutron {
        match best_boost_here {
            BoostType::Neutron => {
                possible_jumps.push((params.jump_range * 4.0, BoostType::Neutron))
            }
            BoostType::WhiteDwarf => {
                possible_jumps.push((params.jump_range * 1.5, BoostType::WhiteDwarf))
            }
            BoostType::None => {}
        }
    }

    let mut all_results = Vec::new();
    for (jump_range, boost_type) in possible_jumps {
        let neighbors = rtree.locate_within_distance(current_system.clone(), jump_range.powi(2));

        let results_for_range: Vec<_> = neighbors
            .par_bridge()
            .filter_map(|neighbor_system| {
                evaluate_neighbor(
                    current_system,
                    neighbor_system,
                    current_g,
                    fuel_after_scoop,
                    jump_range,
                    boost_type,
                    params,
                    rtree,
                    goal_node,
                )
            })
            .collect();
        all_results.extend(results_for_range);
    }
    all_results
}

/// Contains all the detailed logic for evaluating a single potential neighbor.
#[allow(clippy::too_many_arguments)] // This function has many params, which is OK for a helper.
fn evaluate_neighbor(
    current_system: &SystemPoint,
    neighbor_system: &SystemPoint,
    current_g: f64,
    fuel_after_scoop: f64,
    jump_range: f64,
    boost_type: BoostType,
    params: &SearchParameters,
    rtree: &RTree<SystemPoint>,
    goal_node: &SystemPoint,
) -> Option<(usize, f64, f64, f64, JumpDetails)> {
    let neighbor_idx = neighbor_system.idx;
    if neighbor_idx == current_system.idx {
        return None;
    }

    let jump_dist = heuristic(current_system, neighbor_system);
    if jump_dist > jump_range {
        return None;
    }

    // --- Fuel Logic ---
    let fuel_used = calculate_fuel_for_jump(jump_dist, params.jump_range);
    let fuel_after_jump = fuel_after_scoop - fuel_used;
    if fuel_after_jump < 0.0 {
        return None;
    }
    if !is_system_scoopable(neighbor_system, params) {
        let closest_scoopable = rtree
            .nearest_neighbor_iter(neighbor_system)
            .find(|p| is_system_scoopable(p, params));
        if let Some(escape_route_star) = closest_scoopable {
            let escape_dist = heuristic(neighbor_system, escape_route_star);
            let escape_fuel_cost = calculate_fuel_for_jump(escape_dist, params.jump_range);
            if fuel_after_jump < escape_fuel_cost {
                return None;
            }
        }
    }

    // --- G-Score (Jump Cost) Logic ---
    let allowed_boost_at_destination = system_boost_allowed(neighbor_system, params);

    // The cost of a jump is now a combination of the boost used to get there,
    // and the potential for a boost from the destination.
    let jump_cost = match (boost_type, allowed_boost_at_destination) {
        // A boosted jump to another booster is the best possible move.
        (BoostType::Neutron, BoostType::Neutron) => 0.20,
        // A boosted jump is always highly valuable.
        (BoostType::Neutron, _) => 0.25,
        // A conventional jump TO a neutron star is very valuable. This is the key fix.
        (BoostType::None, BoostType::Neutron) => 0.5,
        // A conventional jump to a WD is also valuable.
        (BoostType::None, BoostType::WhiteDwarf) => 0.8,
        // Any other conventional jump has a standard cost.
        _ => 1.0,
    };
    let tentative_g_score = current_g + jump_cost;

    // --- H-Score (Heuristic) and F-Score Logic ---
    let allowed_boost = system_boost_allowed(neighbor_system, params);
    let heuristic_jump_range = match allowed_boost {
        BoostType::Neutron => params.jump_range * 4.0,
        BoostType::WhiteDwarf => params.jump_range * 1.5,
        BoostType::None => params.jump_range,
    };
    let h_score = heuristic(neighbor_system, goal_node) / heuristic_jump_range;
    let f_score = tentative_g_score + (h_score * params.greediness);
    let details = JumpDetails {
        jump_distance: jump_dist,
        fuel_used,
        is_refuel_stop: is_system_scoopable(current_system, params),
        boost_type,
        warnings: Vec::new(),
    };

    Some((
        neighbor_idx,
        tentative_g_score,
        fuel_after_jump,
        f_score,
        details,
    ))
}

fn reconstruct_path(
    goal_idx: usize,
    came_from: &HashMap<usize, (usize, JumpDetails)>,
    start_idx: usize,
    fuel_capacity: f64,
    node_cache: &mut NodeCache,
) -> Vec<JumpResult> {
    let mut path = Vec::new();
    let mut current_idx = goal_idx;

    // Trace the path backwards from goal to start
    while let Some((prev_idx, details)) = came_from.get(&current_idx) {
        let result = JumpResult {
            system_id: node_cache.get(current_idx).id,
            system_idx: current_idx,
            jump_distance: details.jump_distance,
            fuel_used: details.fuel_used,
            fuel_remaining: 0.0, // We will calculate this in a forward pass
            is_refuel_stop: details.is_refuel_stop,
            boost_type: details.boost_type,
            warnings: details.warnings.clone(),
        };
        path.push(result);
        current_idx = *prev_idx;
    }

    // Add the starting system
    path.push(JumpResult {
        system_id: node_cache.get(start_idx).id,
        system_idx: start_idx,
        jump_distance: 0.0,
        fuel_used: 0.0,
        fuel_remaining: fuel_capacity,
        is_refuel_stop: false, // Start isn't a refuel "stop"
        boost_type: BoostType::None,
        warnings: vec![],
    });

    // Reverse the path to go from start to goal
    path.reverse();

    // Now, do a forward pass to calculate fuel remaining correctly
    for i in 1..path.len() {
        let fuel_after_last_jump = path[i - 1].fuel_remaining;
        let details = &came_from.get(&path[i].system_idx).unwrap().1;

        let fuel_at_start_of_jump = if details.is_refuel_stop {
            fuel_capacity
        } else {
            fuel_after_last_jump
        };

        path[i].fuel_remaining = fuel_at_start_of_jump - details.fuel_used;
    }

    path
}

// --- Internal Structs and Enums ---

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, SerdeDeserialize)]
pub enum BoostType {
    None,
    WhiteDwarf,
    Neutron,
}

/// Represents a single jump in the path, stored in the `came_from` map.
#[derive(Clone, Debug)]
struct JumpDetails {
    jump_distance: f64,
    fuel_used: f64,
    is_refuel_stop: bool,
    boost_type: BoostType,
    warnings: Vec<String>,
}

/// Represents the state of a node in the search priority queue.
#[derive(PartialEq)]
struct State {
    cost: f64,
    position_idx: usize,
    fuel: f64,
    f_score: f64,
}

impl Eq for State {}

// This is a more robust implementation for the priority queue ordering.
impl Ord for State {
    fn cmp(&self, other: &Self) -> std::cmp::Ordering {
        // We use `other.cmp(self)` to make the BinaryHeap a min-heap.
        // A state is "greater" if its f_score is smaller.
        if other.f_score < self.f_score {
            std::cmp::Ordering::Less
        } else if other.f_score > self.f_score {
            std::cmp::Ordering::Greater
        } else {
            // As a tie-breaker, we can prefer nodes with a smaller g_score,
            // which can sometimes find shorter paths faster in uniform-cost graphs.
            other
                .cost
                .partial_cmp(&self.cost)
                .unwrap_or(std::cmp::Ordering::Equal)
        }
    }
}

impl PartialOrd for State {
    fn partial_cmp(&self, other: &Self) -> Option<std::cmp::Ordering> {
        Some(self.cmp(other))
    }
}

// --- Helper Functions ---

/// Analyzes a system to find the closest scoopable and best boost options
/// within the maximum allowed star distance.
fn analyze_system_features(system: &SystemPoint, params: &SearchParameters) -> (bool, BoostType) {
    let is_scoopable_here = is_system_scoopable(system, params);
    let boost_type = system_boost_allowed(system, params);

    (is_scoopable_here, boost_type)
}

fn is_system_scoopable(system: &SystemPoint, params: &SearchParameters) -> bool {
    // Check for a scoopable star within the allowed distance.
    // The .map_or() method is a clean way to handle the Option:
    // - If it's None, return the default (false).
    // - If it's Some(dist), run the closure to check the distance.
    system
        .get_closest_scoopable_ls()
        .is_some_and(|dist| (dist as f64) <= params.max_star_distance)
}

fn system_boost_allowed(system: &SystemPoint, params: &SearchParameters) -> BoostType {
    // Determine the best available boost type within the allowed distance.
    if params.use_neutron {
        if let Some(dist) = system.get_closest_neutron_ls() {
            if (dist as f64) <= params.max_star_distance {
                // We have a neutron star in range. This is the best option.
                return BoostType::Neutron;
            }
        }
    }
    // If we get here, there was no neutron star in range.
    // Fall through to check for a white dwarf.
    if !params.exclude_white_dwarfs {
        if let Some(dist) = system.get_closest_white_dwarf_ls() {
            if (dist as f64) <= params.max_star_distance {
                return BoostType::WhiteDwarf;
            }
        }
    }
    BoostType::None
}

#[inline]
fn heuristic(a: &SystemPoint, b: &SystemPoint) -> f64 {
    let dx = a.x - b.x;
    let dy = a.y - b.y;
    let dz = a.z - b.z;
    (dx * dx + dy * dy + dz * dz).sqrt()
}

fn calculate_fuel_for_jump(jump_dist: f64, max_jump_range: f64) -> f64 {
    if max_jump_range <= 0.0 {
        return 0.0;
    }
    const EXPONENT: f64 = 2.2;
    const MAX_FUEL_PER_JUMP: f64 = 8.0;
    let fuel_cost = MAX_FUEL_PER_JUMP * (jump_dist / max_jump_range).powf(EXPONENT);
    fuel_cost.min(MAX_FUEL_PER_JUMP)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::cache::{FullCacheData, SecondaryStar, StarFlags}; // Add this use statement

    fn create_test_system(id: i64, x: f64, y: f64, z: f64, flags: u8) -> SystemPoint {
        SystemPoint {
            id,
            idx: 0, // Placeholder, will be overwritten.
            x,
            y,
            z,
            flags,
            secondary_stars: Vec::new(),
        }
    }

    fn build_id_to_idx_map(points: &[SystemPoint]) -> HashMap<i64, usize> {
        points.iter().enumerate().map(|(i, p)| (p.id, i)).collect()
    }

    /// A test-specific wrapper that takes a simple Vec<SystemPoint>,
    /// serializes it, builds the required data structures, and then calls
    /// the real find_route function.
    fn find_route_for_test(
        params: &SearchParameters,
        mut points: Vec<SystemPoint>, // Take by value and make mutable
    ) -> Option<Vec<JumpResult>> {
        for (i, point) in points.iter_mut().enumerate() {
            point.idx = i;
        }

        let rtree = RTree::bulk_load(points.clone());

        let cache_data = FullCacheData {
            points, // Consumes the original points vec
            stats: Default::default(),
        };
        let buffer = rkyv::to_bytes::<_, 8192>(&cache_data).unwrap();
        let archived_data = unsafe { rkyv::archived_root::<FullCacheData>(&buffer[..]) };

        find_route(params, &rtree, &archived_data.points)
    }

    #[test]
    fn test_find_route_simple_path() {
        let points = vec![
            create_test_system(1, 0.0, 0.0, 0.0, 0),
            create_test_system(2, 10.0, 0.0, 0.0, 0),
            create_test_system(3, 20.0, 0.0, 0.0, 0),
            create_test_system(4, 30.0, 0.0, 0.0, 0),
        ];

        let id_to_idx = build_id_to_idx_map(&points);
        let params = SearchParameters {
            start_idx: *id_to_idx.get(&1).unwrap(),
            end_idx: *id_to_idx.get(&4).unwrap(),
            jump_range: 11.0,
            fuel_capacity: 32.0,
            use_neutron: false,
            exclude_white_dwarfs: true,
            max_star_distance: 1000.0,
            greediness: 1.0,
        };

        let result = find_route_for_test(&params, points);

        assert!(result.is_some());
        let path = result.unwrap();
        assert_eq!(path.len(), 4);
        let path_ids: Vec<i64> = path.iter().map(|p| p.system_id).collect();
        assert_eq!(path_ids, vec![1, 2, 3, 4]);
    }

    #[test]
    fn test_find_route_with_neutron_boost() {
        let points = vec![
            // Start system, has a neutron star for boosting
            create_test_system(1, 0.0, 0.0, 0.0, StarFlags::Neutron as u8),
            // A series of intermediate systems for a non-boosted path
            create_test_system(10, 50.0, 0.0, 0.0, 0),
            create_test_system(11, 100.0, 0.0, 0.0, 0),
            create_test_system(12, 150.0, 0.0, 0.0, 0),
            // A system far away, only reachable with a boosted jump
            create_test_system(2, 190.0, 0.0, 0.0, 0),
            // The goal, right next to the far-away system
            create_test_system(3, 200.0, 0.0, 0.0, 0),
        ];

        let id_to_idx = build_id_to_idx_map(&points);
        let params = SearchParameters {
            start_idx: *id_to_idx.get(&1).unwrap(),
            end_idx: *id_to_idx.get(&3).unwrap(),
            jump_range: 55.0, // Normal jump range is 55 Ly
            fuel_capacity: 32.0,
            use_neutron: true,
            exclude_white_dwarfs: true,
            max_star_distance: 1000.0,
            greediness: 1.0,
        };

        // A normal jump can go 55. A neutron boost can go 55 * 4 = 220.
        // Path A->...->D would be 5 jumps (1->10->11->12->...->3)
        // Path A->C->D should be 1 jump (1->3), using a 200 Ly boosted jump.

        let result = find_route_for_test(&params, points);

        assert!(result.is_some());
        let path = result.unwrap();

        let path_ids: Vec<i64> = path.iter().map(|p| p.system_id).collect();
        assert_eq!(
            path_ids,
            vec![1, 3],
            "The path should take the single-jump neutron boost"
        );
        assert_eq!(path.len(), 2, "The path should have 2 systems (1 jump)");

        // Check that the correct boost type was recorded for the jump
        assert_eq!(path[1].boost_type, BoostType::Neutron);
    }

    #[test]
    fn test_neutron_route_is_now_optimal() {
        let scoop_flag = StarFlags::Scoopable as u8;
        let points = vec![
            // Start: Scoopable + Neutron
            create_test_system(1, 0.0, 0.0, 0.0, StarFlags::Neutron as u8 | scoop_flag),
            // The direct neutron-boost destination
            create_test_system(3, 190.0, 0.0, 0.0, 0),
            // Conventional route: shorter jumps, all scoopable.
            // We move these stars to y=100 so they are not in boost range from the start.
            create_test_system(10, 50.0, 100.0, 0.0, scoop_flag),
            create_test_system(11, 100.0, 100.0, 0.0, scoop_flag),
            create_test_system(12, 150.0, 100.0, 0.0, scoop_flag),
        ];

        let id_to_idx = build_id_to_idx_map(&points);
        let params = SearchParameters {
            start_idx: *id_to_idx.get(&1).unwrap(),
            end_idx: *id_to_idx.get(&3).unwrap(),
            jump_range: 60.0,
            fuel_capacity: 32.0, // <-- Give the ship a standard, larger tank
            use_neutron: true,
            exclude_white_dwarfs: true,
            max_star_distance: 1000.0,
            greediness: 1.0,
        };

        let result = find_route_for_test(&params, points);

        assert!(result.is_some());
        let path = result.unwrap();
        let path_ids: Vec<i64> = path.iter().map(|p| p.system_id).collect();

        // The assertion is now correct because the unintended shortcuts have been removed.
        assert_eq!(
            path_ids,
            vec![1, 3],
            "Path should take the single-jump neutron shortcut now that the logic is fixed"
        );
        assert_eq!(path.len(), 2);
    }

    #[test]
    fn test_neutron_route_complex() {
        let scoop_flag = StarFlags::Scoopable as u8;
        let neutron_flag = StarFlags::Neutron as u8;
        let points = vec![
            // Optimal path:
            // 1 -> 2 -> 3 -> 7 -> 11 -> 12 -> 16 -> 17.
            // Start: normal star, not scoopable.
            create_test_system(1, 0.0, 0.0, 0.0, 0),
            create_test_system(2, 10.0, 0.0, 0.0, 0),
            create_test_system(3, 18.0, 0.0, 0.0, neutron_flag), // Jump range 40 (+18) = 58
            create_test_system(4, 20.0, 0.0, 0.0, 0),
            create_test_system(5, 30.0, 0.0, 0.0, scoop_flag),
            create_test_system(6, 40.0, 0.0, 0.0, 0),
            create_test_system(7, 50.0, 0.0, 0.0, neutron_flag), // Jump range 40 (+50) = 90
            create_test_system(8, 60.0, 0.0, 0.0, scoop_flag),
            create_test_system(9, 70.0, 0.0, 0.0, 0),
            create_test_system(10, 80.0, 0.0, 0.0, scoop_flag),
            create_test_system(11, 90.0, 0.0, 0.0, scoop_flag),
            create_test_system(12, 100.0, 0.0, 0.0, neutron_flag), // Jump range 40 (+100) = 140
            create_test_system(13, 110.0, 0.0, 0.0, scoop_flag),
            create_test_system(14, 120.0, 0.0, 0.0, scoop_flag),
            create_test_system(15, 130.0, 0.0, 0.0, scoop_flag),
            create_test_system(16, 140.0, 0.0, 0.0, scoop_flag), // Should jump here, in optimal.
            // End, not_scoopable.
            create_test_system(17, 150.0, 0.0, 0.0, 0),
        ];

        let id_to_idx = build_id_to_idx_map(&points);
        let params = SearchParameters {
            start_idx: *id_to_idx.get(&1).unwrap(),
            end_idx: *id_to_idx.get(&17).unwrap(),
            jump_range: 10.0,
            fuel_capacity: 32.0,
            use_neutron: true,
            exclude_white_dwarfs: true,
            max_star_distance: 1000.0,
            greediness: 1.0,
        };

        let result = find_route_for_test(&params, points);

        assert!(result.is_some());
        let path = result.unwrap();
        let path_ids: Vec<i64> = path.iter().map(|p| p.system_id).collect();

        // Assert that it now takes the optimal short path.
        assert_eq!(
            path_ids,
            vec![1, 2, 3, 7, 11, 12, 16, 17],
            "Path should use neutron boosts to reduce total number of jumps"
        );
        assert_eq!(path.len(), 8);
    }

    #[test]
    fn test_fuel_required() {
        let fuel_required = calculate_fuel_for_jump(35.5, 50.0);
        assert_eq!(
            3.7658095648305974, fuel_required,
            "we should need this much fuel"
        );
    }

    #[test]
    fn test_correct_aggressive_pruning_trap() {
        let scoop_flag = StarFlags::Scoopable as u8;
        let points = vec![
            // Path A (Optimal): A -> B -> C
            create_test_system(1, 0.0, 0.0, 0.0, scoop_flag), // A: Start
            create_test_system(2, 35.0, 0.0, 0.0, 0),         // B: Intermediate
            create_test_system(3, 70.0, 0.0, 0.0, scoop_flag), // C: Goal
            // Path B (Suboptimal): A -> D -> E -> C
            create_test_system(10, 0.0, 100.0, 0.0, scoop_flag), // D: Detour 1
            create_test_system(11, 70.0, 100.0, 0.0, scoop_flag), // E: Detour 2
        ];

        let id_to_idx = build_id_to_idx_map(&points);
        let params = SearchParameters {
            start_idx: *id_to_idx.get(&1).unwrap(),
            end_idx: *id_to_idx.get(&3).unwrap(),
            jump_range: 50.0,
            fuel_capacity: 10.0, // <-- Key change: Plausible fuel tank size
            use_neutron: false,
            exclude_white_dwarfs: true,
            max_star_distance: 1000.0,
            greediness: 1.0,
        };

        let result = find_route_for_test(&params, points);

        assert!(result.is_some(), "A path should be found");
        let path = result.unwrap();
        let path_ids: Vec<i64> = path.iter().map(|p| p.system_id).collect();

        // The current algorithm will prune 1->2 and be forced to take the longer 1->10->11->3 path.
        // A correct algorithm should find the 1->2->3 path.
        assert_eq!(
            path_ids,
            vec![1, 2, 3],
            "The algorithm should NOT prune the viable two-hop path"
        );
    }

    #[test]
    fn test_wd_route_complex() {
        let scoop_flag = StarFlags::Scoopable as u8;
        let wd_flag = StarFlags::WhiteDwarf as u8;
        let points = vec![
            // Optimal path uses a chain of White Dwarf boosts:
            // 1 -> 2 -> 3(WD) -> 7(WD) -> 11 -> 12
            create_test_system(1, 0.0, 0.0, 0.0, 0),
            create_test_system(2, 8.0, 0.0, 0.0, 0),
            create_test_system(3, 15.0, 0.0, 0.0, wd_flag), // Jump to here is < 10 Ly
            // Suboptimal conventional path (the "trap")
            create_test_system(4, 17.0, 5.0, 0.0, 0),
            create_test_system(5, 25.0, 5.0, 0.0, scoop_flag),
            create_test_system(6, 35.0, 5.0, 0.0, 0),
            // Next stop on the WD highway. Dist from #3 is 14 Ly (>10, <15).
            create_test_system(7, 29.0, 0.0, 0.0, wd_flag),
            // Some other conventional stars
            create_test_system(8, 33.0, -5.0, 0.0, scoop_flag),
            create_test_system(9, 40.0, -5.0, 0.0, 0),
            // Next stop on the WD highway. Dist from #7 is 14 Ly (>10, <15).
            create_test_system(11, 43.0, 0.0, 0.0, scoop_flag),
            // Goal. Dist from #11 is 8 Ly.
            create_test_system(12, 51.0, 0.0, 0.0, 0),
        ];

        let id_to_idx = build_id_to_idx_map(&points);
        let params = SearchParameters {
            start_idx: *id_to_idx.get(&1).unwrap(),
            end_idx: *id_to_idx.get(&12).unwrap(),
            jump_range: 10.0, // Normal jump range is 10 Ly
            fuel_capacity: 32.0,
            use_neutron: true, // Can be true, but no neutrons exist in this test
            exclude_white_dwarfs: false, // CRUCIAL: We must allow white dwarfs
            max_star_distance: 1000.0,
            greediness: 1.0,
        };

        let result = find_route_for_test(&params, points);

        assert!(result.is_some(), "A path should be found");
        let path = result.unwrap();
        let path_ids: Vec<i64> = path.iter().map(|p| p.system_id).collect();

        // With a 10 Ly range, a WD boost is 15 Ly. The 14 Ly gaps can only be
        // crossed with boosts.
        let expected_path = vec![1, 2, 3, 7, 11, 12];
        assert_eq!(
            path_ids, expected_path,
            "Path should use White Dwarf boosts to find the optimal route"
        );
        assert_eq!(path.len(), expected_path.len());
    }

    #[test]
    fn test_mixed_boost_route_complex() {
        let scoop_flag = StarFlags::Scoopable as u8;
        let wd_flag = StarFlags::WhiteDwarf as u8;
        let neutron_flag = StarFlags::Neutron as u8;

        let points = vec![
            // The optimal path requires a WD boost, then later a Neutron boost.
            // Path: 1 -> 2 -> 3(WD) -> 5 -> 6 -> 7(N) -> 9 -> 10
            create_test_system(1, 0.0, 0.0, 0.0, 0),
            create_test_system(2, 8.0, 0.0, 0.0, 0),
            create_test_system(3, 15.0, 0.0, 0.0, wd_flag), // First boost point
            // A longer, suboptimal conventional "trap" path
            create_test_system(4, 10.0, 15.0, 0.0, scoop_flag),
            // Next stop on the highway. Dist from #3 is 14 Ly. Only reachable with WD boost.
            create_test_system(5, 29.0, 0.0, 0.0, scoop_flag),
            create_test_system(6, 38.0, 0.0, 0.0, 0),
            create_test_system(7, 45.0, 0.0, 0.0, neutron_flag), // Second boost point
            // Another conventional trap star
            create_test_system(8, 55.0, -15.0, 0.0, scoop_flag),
            // Final leg. Dist from #7 is 38 Ly. Only reachable with Neutron boost.
            create_test_system(9, 83.0, 0.0, 0.0, scoop_flag),
            create_test_system(10, 90.0, 0.0, 0.0, 0), // Goal
        ];

        let id_to_idx = build_id_to_idx_map(&points);
        let params = SearchParameters {
            start_idx: *id_to_idx.get(&1).unwrap(),
            end_idx: *id_to_idx.get(&10).unwrap(),
            jump_range: 10.0, // Base jump range
            fuel_capacity: 32.0,
            use_neutron: true,
            exclude_white_dwarfs: false, // Must allow both boost types
            max_star_distance: 1000.0,
            greediness: 1.0,
        };

        let result = find_route_for_test(&params, points);

        assert!(result.is_some(), "A path should be found");
        let path = result.unwrap();
        let path_ids: Vec<i64> = path.iter().map(|p| p.system_id).collect();

        // With a 10 Ly range, WD boost is 15, Neutron is 40.
        // The path must use the WD to cross the first 14 Ly gap,
        // and the Neutron to cross the final 38 Ly gap.
        let expected_path = vec![1, 2, 3, 5, 6, 7, 9, 10];
        assert_eq!(
            path_ids, expected_path,
            "Path should use both WD and Neutron boosts for the optimal route"
        );

        // Also verify that the correct boost types were recorded for the jumps
        let jump_to_5 = &path[3]; // Path is [1, 2, 3, 5, ...], so index 3 is system 5
        assert_eq!(
            jump_to_5.boost_type,
            BoostType::WhiteDwarf,
            "Jump from system 3 should be a WD boost"
        );

        let jump_to_9 = &path[6]; // Path index 6 is system 9
        assert_eq!(
            jump_to_9.boost_type,
            BoostType::Neutron,
            "Jump from system 7 should be a Neutron boost"
        );
    }

    #[test]
    fn test_neutron_highway_refuel_detour() {
        let scoop_flag = StarFlags::Scoopable as u8;
        let neutron_flag = StarFlags::Neutron as u8;
        let points = vec![
            // Start at a scoopable star with a full tank.
            create_test_system(1, 0.0, 0.0, 0.0, scoop_flag),
            // A short conventional jump to the "neutron highway".
            create_test_system(10, 50.0, 0.0, 0.0, neutron_flag),
            // --- Path A: The Death Trap ---
            // A tempting but non-scoopable neutron star far away.
            create_test_system(11, 280.0, 0.0, 0.0, neutron_flag),
            // --- Path B: The Smart Detour ---
            // A boring but scoopable star just off the main path.
            create_test_system(20, 50.0, 15.0, 0.0, scoop_flag),
            // The Goal, reachable from the detour star.
            create_test_system(99, 50.0, 15.0, 60.0, scoop_flag),
        ];

        let id_to_idx = build_id_to_idx_map(&points);
        let params = SearchParameters {
            start_idx: *id_to_idx.get(&1).unwrap(),
            end_idx: *id_to_idx.get(&99).unwrap(),
            jump_range: 60.0,    // Boosted range is 240 Ly
            fuel_capacity: 10.0, // A small tank that cannot make two max-cost jumps
            use_neutron: true,
            exclude_white_dwarfs: true,
            max_star_distance: 1000.0,
            greediness: 1.0,
        };

        let result = find_route_for_test(&params, points);

        assert!(result.is_some(), "A path should be found");
        let path = result.unwrap();
        let path_ids: Vec<i64> = path.iter().map(|p| p.system_id).collect();

        assert_eq!(
               path_ids,
               vec![1, 10, 20, 99],
               "Path should take the safe detour to the scoopable star (20) instead of the neutron trap (11)"
           );
    }

    #[test]
    fn test_zero_max_dist_safety_check() {
        let scoop_flag = StarFlags::Scoopable as u8;
        let neutron_flag = StarFlags::Neutron as u8;

        // We need to manually create one of the systems for this test.
        let mut points = vec![
            create_test_system(1, 0.0, 0.0, 0.0, scoop_flag),
            // The only path is via this non-scoopable neutron star.
            create_test_system(10, 50.0, 0.0, 0.0, neutron_flag),
            // The goal is a boosted jump away.
            create_test_system(99, 250.0, 0.0, 0.0, 0),
        ];

        // This is the "safe harbor". Its primary star is NOT scoopable,
        // but it has a secondary star that is.
        let safe_harbor = SystemPoint {
            id: 20,
            idx: 20,
            x: 50.0,
            y: 10.0,
            z: 0.0,
            flags: 0, // Primary star is not scoopable
            secondary_stars: vec![SecondaryStar {
                distance_ls: 500.0, // Secondary star is scoopable
                flags: scoop_flag,
            }],
        };
        points.push(safe_harbor);

        let id_to_idx = build_id_to_idx_map(&points);
        let params = SearchParameters {
            start_idx: *id_to_idx.get(&1).unwrap(),
            end_idx: *id_to_idx.get(&99).unwrap(),
            jump_range: 60.0,
            fuel_capacity: 32.0,
            use_neutron: true,
            exclude_white_dwarfs: true,
            max_star_distance: 0.0, // The critical edge case condition
            greediness: 1.0,
        };

        let result = find_route_for_test(&params, points);

        assert!(
            result.is_some(),
            "A path should be found even with max_star_distance = 0"
        );
        let path = result.unwrap();
        let path_ids: Vec<i64> = path.iter().map(|p| p.system_id).collect();

        assert_eq!(
                path_ids,
                vec![1, 10, 99],
                "The path should be able to use a non-scoopable star by correctly identifying a safe harbor with a secondary scoopable star."
            );
    }

    #[test]
    fn test_void_avoidance_and_neutron_trap() {
        let scoop_flag = StarFlags::Scoopable as u8;
        let neutron_flag = StarFlags::Neutron as u8;

        let points = vec![
            // Start
            create_test_system(1, 0.0, 0.0, 0.0, scoop_flag),
            create_test_system(2, 60.0, 0.0, 0.0, scoop_flag),
            // --- Path A: The Long, Safe Way Around (Corrected Coordinates) ---
            // All jumps in this chain are now possible within an 80 Ly range.
            create_test_system(10, 100.0, 60.0, 0.0, scoop_flag),
            create_test_system(11, 150.0, 100.0, 0.0, scoop_flag),
            create_test_system(12, 200.0, 60.0, 0.0, scoop_flag),
            create_test_system(13, 240.0, 0.0, 0.0, scoop_flag),
            // --- Path B: The Siren Star (Dead End) ---
            // A neutron star in the void. Reachable from system 2, but a dead end.
            create_test_system(50, 150.0, 0.0, 0.0, neutron_flag),
            // The Goal, on the other side of the void.
            create_test_system(99, 300.0, 0.0, 0.0, scoop_flag),
        ];

        let id_to_idx = build_id_to_idx_map(&points);
        let params = SearchParameters {
            start_idx: *id_to_idx.get(&1).unwrap(),
            end_idx: *id_to_idx.get(&99).unwrap(),
            jump_range: 80.0, // Increased to make the trap reachable
            fuel_capacity: 14.0,
            use_neutron: true,
            exclude_white_dwarfs: true,
            max_star_distance: 1000.0,
            greediness: 1.0,
        };

        // --- THE LOGIC OF THE TRAP ---
        // 1. From system 2, the algorithm can jump to the Siren Star (50), which looks
        //    like a great shortcut.
        // 2. However, the algorithm's smart fuel check will correctly determine that from
        //    system 50, there are no scoopable stars within its remaining fuel range.
        //    It will prune this path as a dead end.
        // 3. The algorithm will then be forced to explore the longer, but fully viable,
        //    path that goes around the void (10 -> 11 -> 12 -> 13).

        let result = find_route_for_test(&params, points);

        assert!(result.is_some(), "A path around the void should be found");
        let path = result.unwrap();
        let path_ids: Vec<i64> = path.iter().map(|p| p.system_id).collect();

        // The path must not contain the dead-end neutron star.
        assert!(
            !path_ids.contains(&50),
            "The path must avoid the dead-end neutron star trap"
        );

        // The path must be the longer, safe route.
        assert_eq!(
            path_ids,
            vec![1, 2, 10, 11, 12, 13, 99],
            "Path should go around the void using the safe, scoopable route"
        );
    }
}
