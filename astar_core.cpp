#include "astar_core.h"
#include <iostream>
#include <fstream>
#include <cmath>
#include <queue>
#include <unordered_map>
#include <tuple>
#include <stdexcept>
#include <chrono>
#include <algorithm>
#include <thread>
#include <mutex>
#include <condition_variable>
#include <sqlite3.h>

// SIMD Intrinsics header for SSE2
#include <emmintrin.h>

// Boost Geometry headers ONLY
#include <boost/geometry.hpp>
#include <boost/geometry/geometries/point.hpp>
#include <boost/geometry/geometries/box.hpp>
#include <boost/geometry/index/rtree.hpp>

// Boost Serialization headers (only needed for loading cache)
#include <boost/archive/binary_iarchive.hpp>
#include <boost/serialization/vector.hpp>
#include <boost/serialization/unordered_map.hpp>
#include <boost/serialization/utility.hpp>
#include <boost/serialization/split_free.hpp>

namespace bg = boost::geometry;
namespace bgi = boost::geometry::index;

// --- Internal Data Structures ---

using point = bg::model::point<double, 3, bg::cs::cartesian>;
using box = bg::model::box<point>;
using value = std::pair<point, long long>;

struct Coords {
    double x, y, z;
    template<class Archive>
    void serialize(Archive & ar, const unsigned int version) { ar & x & y & z; }
};

struct StarInfo {
    uint8_t flags;
    double distance_ls;
    template<class Archive>
    void serialize(Archive & ar, const unsigned int version) { ar & flags & distance_ls; }
};

const uint8_t IS_SCOOPABLE = 1 << 0;
const uint8_t IS_NEUTRON = 1 << 1;
const uint8_t IS_WHITE_DWARF = 1 << 2;

struct SystemCacheInfo {
    Coords coords;
    std::vector<StarInfo> stars;
    template<class Archive>
    void serialize(Archive & ar, const unsigned int version) { ar & coords & stars; }
};

enum class BoostType { NONE, NEUTRON, WHITE_DWARF };

struct JumpDetails {
    double jump_distance;
    double fuel_used;
    double fuel_remaining;
    bool is_refuel_stop;
    BoostType boost_type;
    std::vector<std::string> warnings;
};

// --- Serialization support for Boost Geometry Point ---
namespace boost { namespace serialization {
template<class Archive>
void save(Archive & ar, const point & p, const unsigned int version) {
    ar & bg::get<0>(p); ar & bg::get<1>(p); ar & bg::get<2>(p);
}
template<class Archive>
void load(Archive & ar, point & p, const unsigned int version) {
    double x, y, z;
    ar & x; ar & y; ar & z;
    bg::set<0>(p, x); bg::set<1>(p, y); bg::set<2>(p, z);
}
template<class Archive>
void serialize(Archive & ar, point & p, const unsigned int version) {
    boost::serialization::split_free(ar, p, version);
}
}}

// --- In-Memory Data Stores ---
bgi::rtree<value, bgi::quadratic<16>> rtree;
std::unordered_map<long long, SystemCacheInfo> system_cache;
bool data_loaded = false;

// --- Thread Pool ---
class ThreadPool {
public:
    ThreadPool(size_t threads) : stop(false) {
        for(size_t i = 0; i < threads; ++i)
            workers.emplace_back([this] {
                for(;;) {
                    std::function<void()> task;
                    {
                        std::unique_lock<std::mutex> lock(this->queue_mutex);
                        this->condition.wait(lock, [this]{ return this->stop || !this->tasks.empty(); });
                        if(this->stop && this->tasks.empty()) return;
                        task = std::move(this->tasks.front());
                        this->tasks.pop();
                    }
                    task();
                }
            });
    }
    template<class F>
    void enqueue(F&& f) {
        {
            std::unique_lock<std::mutex> lock(queue_mutex);
            tasks.emplace(std::forward<F>(f));
        }
        condition.notify_one();
    }
    ~ThreadPool() {
        {
            std::unique_lock<std::mutex> lock(queue_mutex);
            stop = true;
        }
        condition.notify_all();
        for(std::thread &worker: workers)
            worker.join();
    }
private:
    std::vector<std::thread> workers;
    std::queue<std::function<void()>> tasks;
    std::mutex queue_mutex;
    std::condition_variable condition;
    bool stop;
};

// --- Helper Functions ---
inline double heuristic(const Coords& a, const Coords& b) {
    __m128d a_xy = _mm_set_pd(a.y, a.x);
    __m128d b_xy = _mm_set_pd(b.y, b.x);
    __m128d a_z = _mm_set_sd(a.z);
    __m128d b_z = _mm_set_sd(b.z);
    __m128d diff_xy = _mm_sub_pd(a_xy, b_xy);
    __m128d diff_z = _mm_sub_sd(a_z, b_z);
    __m128d sq_xy = _mm_mul_pd(diff_xy, diff_xy);
    __m128d sq_z = _mm_mul_sd(diff_z, diff_z);
    __m128d sum_sq = _mm_add_sd(sq_z, _mm_hadd_pd(sq_xy, sq_xy));
    __m128d sqrt_val = _mm_sqrt_sd(_mm_setzero_pd(), sum_sq);
    return _mm_cvtsd_f64(sqrt_val);
}

double calculate_fuel_for_jump(double jump_dist, double max_jump_range) {
    if (max_jump_range <= 0) return 0.0;
    const double exponent = 2.2;
    const double max_fuel_per_jump = 8.0;
    double fuel_cost = max_fuel_per_jump * std::pow(jump_dist / max_jump_range, exponent);
    return std::min(fuel_cost, max_fuel_per_jump);
}

void initialize_in_memory_data(const std::string& cache_path) {
    if (data_loaded) { return; }
    
    auto start_time = std::chrono::steady_clock::now();
    std::cout << "[C++] Attempting to load from binary cache '" << cache_path << "'..." << std::endl;

    std::ifstream ifs(cache_path, std::ios::binary);
    if (ifs) {
        try {
            boost::archive::binary_iarchive ia(ifs);
            std::vector<value> rtree_data;
            ia >> rtree_data;
            rtree = bgi::rtree<value, bgi::quadratic<16>>(rtree_data.begin(), rtree_data.end());
            
            std::vector<std::pair<long long, SystemCacheInfo>> cache_as_vector;
            ia >> cache_as_vector;
            system_cache = std::unordered_map<long long, SystemCacheInfo>(cache_as_vector.begin(), cache_as_vector.end());
            
            data_loaded = true;
            auto end_time = std::chrono::steady_clock::now();
            auto duration = std::chrono::duration_cast<std::chrono::milliseconds>(end_time - start_time).count();
            std::cout << "[C++] Successfully loaded data from cache in " << duration << " ms." << std::endl;
        } catch (const std::exception& e) {
            std::cerr << "[C++] WARN: Could not load from cache file: " << e.what() << std::endl;
            rtree.clear();
            system_cache.clear();
        }
    } else {
        throw std::runtime_error("starmap.cache not found. Please run the generate_cache utility first.");
    }
}

using State = std::tuple<double, double, long long, double>;

struct SearchInstance {
    std::priority_queue<State, std::vector<State>, std::greater<State>> open_set;
    std::unordered_map<long long, std::pair<long long, JumpDetails>> came_from;
    std::unordered_map<long long, double> g_score;
    std::unordered_map<long long, double> fuel_at_node;
    long long goal_id;
};

struct NeighborResult {
    long long neighbor_id;
    double tentative_g_score;
    double fuel_after_jump;
    JumpDetails details;
};

std::vector<JumpResult> run_astar_search_core(const SearchParameters& params) {
    if (!data_loaded) {
        throw std::runtime_error("C++ core library has not been initialized.");
    }
    
    auto start_time = std::chrono::steady_clock::now();
    
    if (params.progress_callback) params.progress_callback("[C++] Bi-directional A* search started.");

    const Coords& start_coords = system_cache.at(params.start_id).coords;
    const Coords& goal_coords = system_cache.at(params.end_id).coords;
    
    SearchInstance fwd;
    fwd.goal_id = params.end_id;
    fwd.open_set.push({heuristic(start_coords, goal_coords) / params.jump_range, 0.0, params.start_id, params.fuel_capacity});
    fwd.g_score[params.start_id] = 0.0;
    fwd.fuel_at_node[params.start_id] = params.fuel_capacity;

    SearchInstance bwd;
    bwd.goal_id = params.start_id;
    bwd.open_set.push({heuristic(goal_coords, start_coords) / params.jump_range, 0.0, params.end_id, params.fuel_capacity});
    bwd.g_score[params.end_id] = 0.0;
    bwd.fuel_at_node[params.end_id] = params.fuel_capacity;
    
    long long meeting_point = -1;
    double best_path_score = std::numeric_limits<double>::infinity();
    long long nodes_processed = 0, last_nodes_processed = 0;
    auto last_progress_time = std::chrono::steady_clock::now();
    ThreadPool pool(std::thread::hardware_concurrency());

    while (!fwd.open_set.empty() && !bwd.open_set.empty()) {
        if (params.check_cancelled && params.check_cancelled()) {
            if (params.progress_callback) params.progress_callback("[C++] Search cancelled by user.");
            return {};
        }

        double fwd_best_f = std::get<0>(fwd.open_set.top());
        double bwd_best_f = std::get<0>(bwd.open_set.top());
        if (fwd_best_f + bwd_best_f >= best_path_score) break;

        SearchInstance& current_search = (fwd.open_set.top() < bwd.open_set.top()) ? fwd : bwd;
        SearchInstance& other_search = &current_search == &fwd ? bwd : fwd;

        nodes_processed++;
        auto [f, current_g, current_id, current_fuel] = current_search.open_set.top();
        current_search.open_set.pop();
        
        if (other_search.g_score.count(current_id)) {
            double total_path_score = current_g + other_search.g_score.at(current_id);
            if (total_path_score < best_path_score) {
                best_path_score = total_path_score;
                meeting_point = current_id;
            }
        }
        
        auto now = std::chrono::steady_clock::now();
        if (std::chrono::duration_cast<std::chrono::seconds>(now - last_progress_time).count() >= 2) {
            if (params.progress_callback) {
                double elapsed_seconds = std::chrono::duration_cast<std::chrono::duration<double>>(now - last_progress_time).count();
                double nodes_per_sec = elapsed_seconds > 0 ? (nodes_processed - last_nodes_processed) / elapsed_seconds : 0;
                last_nodes_processed = nodes_processed;
                
                const Coords& progress_coords = system_cache.at(current_id).coords;
                double h_score = heuristic(progress_coords, system_cache.at(current_search.goal_id).coords) / params.jump_range;
                
                std::string msg = "[DEBUG] Nodes: " + std::to_string(nodes_processed) +
                                  " (" + std::to_string(static_cast<int>(nodes_per_sec)) + " nodes/s)" +
                                  " | Current ID: " + std::to_string(current_id) +
                                  " | g=" + std::to_string(static_cast<int>(current_g)) + 
                                  ", h=" + std::to_string(static_cast<int>(h_score)) +
                                  ", f=" + std::to_string(static_cast<int>(f));

                params.progress_callback(msg);
            }
            last_progress_time = now;
        }

        const SystemCacheInfo& current_system_info = system_cache.at(current_id);
        const Coords& current_coords = current_system_info.coords;
        const std::vector<StarInfo>& current_stars = current_system_info.stars;

        bool is_scoopable_here = false;
        for (const auto& star : current_stars) {
            if ((star.flags & IS_SCOOPABLE) && star.distance_ls <= params.max_star_distance) {
                is_scoopable_here = true;
                break;
            }
        }
        double fuel_after_scoop = is_scoopable_here ? params.fuel_capacity : current_fuel;

        struct JumpType { double range; BoostType boost_type; };
        std::vector<JumpType> possible_jumps = {{params.jump_range, BoostType::NONE}};

        if (params.use_neutron) {
            for (const auto& star : current_stars) {
                if ((star.flags & IS_NEUTRON) && star.distance_ls <= params.max_star_distance) {
                    possible_jumps.push_back({params.jump_range * 4.0, BoostType::NEUTRON}); break; 
                }
                if (!params.exclude_white_dwarfs && (star.flags & IS_WHITE_DWARF) && star.distance_ls <= params.max_star_distance) {
                    possible_jumps.push_back({params.jump_range * 1.5, BoostType::WHITE_DWARF}); break;
                }
            }
        }
        
        for (const auto& jump_type : possible_jumps) {
            box query_box(
                point(current_coords.x - jump_type.range, current_coords.y - jump_type.range, current_coords.z - jump_type.range),
                point(current_coords.x + jump_type.range, current_coords.y + jump_type.range, current_coords.z + jump_type.range)
            );
            std::vector<value> neighbors;
            rtree.query(bgi::intersects(query_box), std::back_inserter(neighbors));
            
            std::vector<NeighborResult> results;
            std::mutex results_mutex;
            std::condition_variable cv;
            size_t tasks_finished = 0;
            
            size_t num_threads = std::thread::hardware_concurrency();
            size_t chunk_size = (neighbors.size() / num_threads) + 1;
            if (chunk_size == 0) continue;
            size_t num_tasks = (neighbors.size() / chunk_size) + (neighbors.size() % chunk_size != 0 ? 1 : 0);

            for (size_t i = 0; i < neighbors.size(); i += chunk_size) {
                 pool.enqueue([&, i, chunk_size]{
                    std::vector<NeighborResult> local_results;
                    size_t end = std::min(i + chunk_size, neighbors.size());
                    for (size_t j = i; j < end; ++j) {
                        const auto& val = neighbors[j];
                        long long neighbor_id = val.second;
                        const SystemCacheInfo& neighbor_info = system_cache.at(neighbor_id);
                        const Coords& neighbor_coords = neighbor_info.coords;
                        double jump_dist = heuristic(current_coords, neighbor_coords);
                        if (jump_dist > jump_type.range) continue;

                        double fuel_used = calculate_fuel_for_jump(jump_dist, params.jump_range);
                        double fuel_after_jump = fuel_after_scoop - fuel_used;
                        if (fuel_after_jump < 0) continue;

                        const std::vector<StarInfo>& neighbor_stars = neighbor_info.stars;
                        
                        bool neighbor_is_scoopable = false;
                        for (const auto& star : neighbor_stars) {
                            if ((star.flags & IS_SCOOPABLE) && star.distance_ls <= params.max_star_distance) {
                                neighbor_is_scoopable = true;
                                break;
                            }
                        }

                        double tentative_g_score = current_g + 1;
                        
                        auto g_it = current_search.g_score.find(neighbor_id);
                        if (g_it != current_search.g_score.end() && tentative_g_score >= g_it->second) continue;
                        
                        JumpDetails details;
                        details.jump_distance = jump_dist;
                        details.fuel_used = fuel_used;
                        details.fuel_remaining = fuel_after_jump;
                        details.is_refuel_stop = is_scoopable_here;
                        details.boost_type = jump_type.boost_type;
                        if (!is_scoopable_here && fuel_after_jump < calculate_fuel_for_jump(params.jump_range, params.jump_range) * 2 && neighbor_is_scoopable) {
                            details.warnings.push_back("Refuel advised at next stop");
                        }
                        local_results.push_back({neighbor_id, tentative_g_score, fuel_after_jump, details});
                    }
                    {
                        std::lock_guard<std::mutex> lock(results_mutex);
                        results.insert(results.end(), local_results.begin(), local_results.end());
                        tasks_finished++;
                    }
                    cv.notify_one();
                 });
            }

            {
                std::unique_lock<std::mutex> lock(results_mutex);
                cv.wait(lock, [&]{ return tasks_finished == num_tasks; });
            }

            for (const auto& res : results) {
                auto g_it = current_search.g_score.find(res.neighbor_id);
                if (g_it != current_search.g_score.end() && res.tentative_g_score >= g_it->second) continue;

                current_search.g_score[res.neighbor_id] = res.tentative_g_score;
                current_search.fuel_at_node[res.neighbor_id] = res.fuel_after_jump;
                current_search.came_from[res.neighbor_id] = {current_id, res.details};
                
                double f_score_val = res.tentative_g_score + heuristic(system_cache.at(res.neighbor_id).coords, system_cache.at(current_search.goal_id).coords) / params.jump_range;
                current_search.open_set.push({f_score_val, res.tentative_g_score, res.neighbor_id, res.fuel_after_jump});
            }
        }
    }

    if (meeting_point != -1) {
        if (params.progress_callback) params.progress_callback("\n[C++] Path found! Reconstructing...");
        
        std::vector<long long> path_ids;
        long long path_id = meeting_point;
        while (fwd.came_from.count(path_id)) {
            path_ids.insert(path_ids.begin(), path_id);
            path_id = fwd.came_from.at(path_id).first;
        }
        path_ids.insert(path_ids.begin(), params.start_id);

        path_id = meeting_point;
        while (bwd.came_from.count(path_id)) {
            path_id = bwd.came_from.at(path_id).first;
            path_ids.push_back(path_id);
        }

        std::vector<JumpResult> final_path;
        double current_fuel_val = params.fuel_capacity;

        sqlite3* db;
        sqlite3_open_v2("starmap.db", &db, SQLITE_OPEN_READONLY, nullptr);

        for (size_t i = 0; i < path_ids.size(); ++i) {
            long long current_sys_id = path_ids[i];
            JumpResult step;
            step.system_id = current_sys_id;

            if (i == 0) {
                 step.jump_distance = 0.0;
                 step.fuel_used = 0.0;
                 step.fuel_remaining = params.fuel_capacity;
                 step.is_refuel_stop = false;
                 step.boost_type = 0;
            } else {
                long long prev_sys_id = path_ids[i-1];
                const JumpDetails& details = fwd.came_from.count(current_sys_id) ? 
                    fwd.came_from.at(current_sys_id).second : bwd.came_from.at(prev_sys_id).second;
                
                 step.jump_distance = details.jump_distance;
                 step.fuel_used = details.fuel_used;
                 
                 if(details.is_refuel_stop) {
                     current_fuel_val = params.fuel_capacity;
                 }
                 current_fuel_val -= details.fuel_used;
                 step.fuel_remaining = current_fuel_val;

                 step.is_refuel_stop = details.is_refuel_stop;
                 step.boost_type = static_cast<int>(details.boost_type);
                 step.warnings = details.warnings;
            }
             final_path.push_back(step);
        }
        
        sqlite3_close(db);

        auto end_time = std::chrono::steady_clock::now();
        auto duration = std::chrono::duration_cast<std::chrono::milliseconds>(end_time - start_time).count();
        if (params.progress_callback) params.progress_callback("[C++] Path reconstruction complete in " + std::to_string(duration/1000.0) + "s total.");
        return final_path;
    }

    if (params.progress_callback) params.progress_callback("[C++] Search finished. No path found.");
    return {};
}

