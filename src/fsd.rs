use serde::{Deserialize as SerdeDeserialize, Serialize};

// Linear Constant / 1000
pub static FSD_MULTIPLIERS: phf::Map<char, f64> = phf::phf_map! {
    'A' => 0.012, // 12 / 1000
    'B' => 0.010, // 10 / 1000
    'C' => 0.008, // 8 / 1000
    'D' => 0.010, // 10 / 1000
    'E' => 0.011, // 11 / 1000
};

// Power Constant
pub static FSD_EXPONENTS: phf::Map<u8, f64> = phf::phf_map! {
    2u8 => 2.00,
    3u8 => 2.15,
    4u8 => 2.30,
    5u8 => 2.45,
    6u8 => 2.60,
    7u8 => 2.75,
    8u8 => 2.90,
};

pub static GUARDIAN_BOOSTERS: phf::Map<u8, f64> = phf::phf_map! {
    1u8 => 4.0,
    2u8 => 6.0,
    3u8 => 7.75,
    4u8 => 9.25,
    5u8 => 10.5,
};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, SerdeDeserialize)]
pub enum BoostType {
    None,
    WhiteDwarf,
    Neutron,
}

#[derive(Debug, Clone, Default)]
pub struct FsdSpec {
    pub ship_mass: f64,
    pub fsd_optimal_mass: f64,
    pub fsd_max_fuel_per_jump: f64,
    pub fsd_multiplier: f64,
    pub fsd_exponent: f64,
    pub guardian_booster_size: Option<u8>,
}

pub fn calculate_fuel_for_jump(
    jump_dist: f64,
    boost_type: BoostType,
    legacy_jump_range: f64,
    fsd_spec: &Option<FsdSpec>,
) -> f64 {
    if let Some(spec) = fsd_spec {
        if spec.fsd_optimal_mass <= 0.0 {
            return 0.0;
        }

        let guardian_boost = spec
            .guardian_booster_size
            .and_then(|size| GUARDIAN_BOOSTERS.get(&size))
            .map_or(0.0, |boost| *boost);

        let dist_after_guardian = (jump_dist - guardian_boost).max(0.0);
        // If we were boosted, we have to get the original unboosted
        // distance for the fuel calculation.
        let effective_dist = match boost_type {
            BoostType::None => dist_after_guardian,
            // Note: A White Dwarf boost is +50%, so a 1.5x multiplier
            BoostType::WhiteDwarf => dist_after_guardian / 1.5,
            BoostType::Neutron => dist_after_guardian / 4.0,
        };

        // Calculate the fuel cost based on the effective distance.
        let calculated_fuel = spec.fsd_multiplier
            * ((effective_dist * spec.ship_mass / spec.fsd_optimal_mass).powf(spec.fsd_exponent));

        // The fuel cost cannot be less than the multiplier itself.
        // This establishes the "floor" or minimum fuel consumption.
        let fuel_with_floor = calculated_fuel.max(spec.fsd_multiplier);

        // Finally, the cost is capped by the FSD's absolute maximum fuel per jump.
        return fuel_with_floor.min(spec.fsd_max_fuel_per_jump);
    }

    if legacy_jump_range <= 0.0 {
        return 0.0;
    }
    const LEGACY_EXPONENT: f64 = 2.2;
    const LEGACY_MAX_FUEL_PER_JUMP: f64 = 8.0;
    let fuel_cost =
        LEGACY_MAX_FUEL_PER_JUMP * (jump_dist / legacy_jump_range).powf(LEGACY_EXPONENT);
    fuel_cost.min(LEGACY_MAX_FUEL_PER_JUMP)
}

/// Calculates the maximum jump range of a ship for a given amount of fuel.
///
/// This is the inverse of the fuel consumption formula.
pub fn calculate_jump_range(spec: &FsdSpec, fuel_for_jump: f64) -> f64 {
    // 1. Get the flat jump range bonus from the Guardian FSD Booster.
    let guardian_boost = spec
        .guardian_booster_size
        .and_then(|size| GUARDIAN_BOOSTERS.get(&size))
        .map_or(0.0, |boost| *boost);

    // 2. The fuel used for the jump cannot exceed the FSD's maximum fuel per jump.
    let capped_fuel = fuel_for_jump.min(spec.fsd_max_fuel_per_jump);

    // 3. Add safety checks to prevent division by zero or other math errors.
    if spec.ship_mass <= 0.0 || spec.fsd_optimal_mass <= 0.0 || spec.fsd_multiplier <= 0.0 {
        return 0.0 + guardian_boost; // Still return the flat boost if applicable
    }

    // 4. Calculate the base jump range by inverting the fuel formula.
    let base_range = (capped_fuel / spec.fsd_multiplier).powf(1.0 / spec.fsd_exponent)
        * spec.fsd_optimal_mass
        / spec.ship_mass;

    // 5. Add the flat Guardian bonus to the calculated base range for the final result.
    base_range + guardian_boost
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_legacy_fuel_calculation_ignores_booster() {
        let jump_range = 50.0;
        let jump_dist = 35.5;
        let fsd_spec = None;
        let fuel_cost = calculate_fuel_for_jump(jump_dist, BoostType::None, jump_range, &fsd_spec);
        assert_eq!(3.7658095648305974, fuel_cost);
    }

    #[test]
    fn test_advanced_fuel_anaconda_guardian_booster_1ly_jump() {
        let fsd_spec = FsdSpec {
            ship_mass: 516.0, // Full fuel tank.
            fsd_max_fuel_per_jump: 8.0,
            fsd_exponent: *FSD_EXPONENTS.get(&6u8).unwrap(),
            fsd_multiplier: *FSD_MULTIPLIERS.get(&'A').unwrap(),
            fsd_optimal_mass: 2902.0,
            guardian_booster_size: Some(5),
        };
        let jump_dist = 1.0; // Jump smaller than Guardian FSD boost.

        let fuel_with_booster =
            calculate_fuel_for_jump(jump_dist, BoostType::None, 0.0, &Some(fsd_spec));
        assert_eq!(fuel_with_booster, 0.012);
    }

    #[test]
    fn test_advanced_fuel_anaconda_guardian_booster() {
        let mut fsd_spec = FsdSpec {
            ship_mass: 516.0, // Full fuel tank.
            fsd_max_fuel_per_jump: 8.0,
            fsd_exponent: *FSD_EXPONENTS.get(&6u8).unwrap(),
            fsd_multiplier: *FSD_MULTIPLIERS.get(&'A').unwrap(),
            fsd_optimal_mass: 2902.0,
            guardian_booster_size: Some(5),
        };
        let jump_dist = 78.99; // Max jump with full fuel.

        let fuel_with_booster =
            calculate_fuel_for_jump(jump_dist, BoostType::None, 0.0, &Some(fsd_spec.clone()));
        assert_eq!(fuel_with_booster, 7.974213172505302);

        // Try max range with only 8T fuel (for 1 jump). (516-24)
        fsd_spec.ship_mass = 492.0;
        let jump_dist = 82.33; // Max jump with only 8T of fuel.
        let fuel_with_booster =
            calculate_fuel_for_jump(jump_dist, BoostType::None, 0.0, &Some(fsd_spec.clone()));

        assert_eq!(fuel_with_booster, 7.9739315801632165);
    }

    #[test]
    fn test_advanced_fuel_anaconda_no_booster() {
        let mut fsd_spec = FsdSpec {
            ship_mass: 514.0, // Full fuel tank.
            fsd_max_fuel_per_jump: 8.0,
            fsd_exponent: *FSD_EXPONENTS.get(&6u8).unwrap(),
            fsd_multiplier: *FSD_MULTIPLIERS.get(&'A').unwrap(),
            fsd_optimal_mass: 2902.0,
            guardian_booster_size: None,
        };
        let jump_dist = 68.66; // Max jump with full fuel.

        let fuel_with_booster =
            calculate_fuel_for_jump(jump_dist, BoostType::None, 0.0, &Some(fsd_spec.clone()));
        assert_eq!(fuel_with_booster, 7.945147642692557);

        // Try max range with only 8T fuel (for 1 jump). (514-24)
        fsd_spec.ship_mass = 490.0;
        let jump_dist = 72.02; // Max jump with only 8T of fuel.
        let fuel_with_booster =
            calculate_fuel_for_jump(jump_dist, BoostType::None, 0.0, &Some(fsd_spec));

        assert_eq!(fuel_with_booster, 7.9443047802209374);
    }

    #[test]
    fn test_advanced_fuel_from_user_case() {
        let fsd_spec = FsdSpec {
            ship_mass: 346.9,
            fsd_max_fuel_per_jump: 5.0,
            fsd_exponent: *FSD_EXPONENTS.get(&5u8).unwrap(),
            fsd_multiplier: *FSD_MULTIPLIERS.get(&'A').unwrap(),
            fsd_optimal_mass: 1692.6,
            guardian_booster_size: Some(5), // 10.50 Ly
        };
        let jump_dist = 67.73366678246619;

        let fuel_cost = calculate_fuel_for_jump(jump_dist, BoostType::None, 0.0, &Some(fsd_spec));

        assert_eq!(fuel_cost, 4.999999999999998);
    }

    #[test]
    fn test_jump_range_calculation_anaconda_no_booster() {
        // This spec mirrors the setup in `test_advanced_fuel_anaconda_no_booster`
        let spec = FsdSpec {
            ship_mass: 490.0,
            fsd_optimal_mass: 2902.0,
            fsd_max_fuel_per_jump: 8.0,
            fsd_exponent: *FSD_EXPONENTS.get(&6u8).unwrap(),
            fsd_multiplier: *FSD_MULTIPLIERS.get(&'A').unwrap(),
            guardian_booster_size: None,
        };

        // Using the max fuel should yield the max jump range.
        let max_fuel = 8.0;
        let range = calculate_jump_range(&spec, max_fuel);

        // This jump range of ~72.02 Ly is the input to the corresponding fuel test.
        // This confirms our calculation is the correct inverse.
        assert!(
            (range - 72.02).abs() < 0.01,
            "Calculated range was {}",
            range
        );
    }

    #[test]
    fn test_jump_range_calculation_with_booster() {
        let mut spec = FsdSpec {
            ship_mass: 310.0,
            fsd_optimal_mass: 1785.0,
            fsd_max_fuel_per_jump: 5.0,
            fsd_exponent: *FSD_EXPONENTS.get(&5u8).unwrap(),
            fsd_multiplier: *FSD_MULTIPLIERS.get(&'A').unwrap(),
            guardian_booster_size: Some(5), // 10.5 Ly boost
        };

        let max_fuel = 5.0;
        let range = calculate_jump_range(&spec, max_fuel);

        // according to coriolis..
        assert_eq!(range, 75.69, "Calculated range was {}", range);
        // according to game...
        //assert_eq!(range, 77.69, "Calculated range was {}", range);

        spec.ship_mass = spec.ship_mass - 32.0 + 5.0;
        let range = calculate_jump_range(&spec, max_fuel);

        // according to coriolis
        assert_eq!(range, 83.95, "Calculated range was {}", range);
    }
}
