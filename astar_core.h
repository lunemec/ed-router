#ifndef ASTAR_CORE_H
#define ASTAR_CORE_H

#include <string>
#include <vector>
#include <cstdint>
#include <functional>

// --- Data Structures for the Public Interface ---

// Parameters passed from Python to the search function
struct SearchParameters {
    long long start_id;
    long long end_id;
    double jump_range;
    double fuel_capacity;
    double max_star_distance;
    bool use_neutron;
    bool exclude_white_dwarfs;
    std::function<void(const std::string&)> progress_callback;
    std::function<bool()> check_cancelled; // Callback to check for shutdown
};

// Simplified result struct returned to the wrapper
struct JumpResult {
    long long system_id;
    double jump_distance;
    double fuel_used;
    double fuel_remaining;
    bool is_refuel_stop;
    int boost_type; // 0=None, 1=Neutron, 2=WD
    std::vector<std::string> warnings;
};

// --- Public Function Declarations ---

// Loads all data into memory from the binary cache.
void initialize_in_memory_data(const std::string& cache_path);

// Runs the high-performance, multi-threaded A* search.
std::vector<JumpResult> run_astar_search_core(const SearchParameters& params);

#endif // ASTAR_CORE_H

