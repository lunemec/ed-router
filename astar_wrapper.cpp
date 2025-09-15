#include <pybind11/pybind11.h>
#include <pybind11/stl.h>
#include <pybind11/functional.h>
#include <sqlite3.h>
#include "astar_core.h" // Include our pure C++ header

namespace py = pybind11;

// Helper to get system names for the final path
std::string get_system_name_by_id(sqlite3* db, long long system_id) {
    sqlite3_stmt* stmt;
    const char* query = "SELECT name FROM systems WHERE id = ?";
    sqlite3_prepare_v2(db, query, -1, &stmt, nullptr);
    sqlite3_bind_int64(stmt, 1, system_id);
    std::string name = "";
    if (sqlite3_step(stmt) == SQLITE_ROW) {
        name = reinterpret_cast<const char*>(sqlite3_column_text(stmt, 0));
    }
    sqlite3_finalize(stmt);
    return name;
}

// The Python-facing function that wraps the C++ core
py::list run_astar_search_wrapper(py::dict params) {
    SearchParameters core_params;
    core_params.start_id = params["start_id"].cast<long long>();
    core_params.end_id = params["end_id"].cast<long long>();
    core_params.jump_range = params["jump_range"].cast<double>();
    core_params.fuel_capacity = params["fuel_capacity"].cast<double>();
    core_params.max_star_distance = params["max_star_distance"].cast<double>();
    core_params.use_neutron = params["use_neutron"].cast<bool>();
    core_params.exclude_white_dwarfs = params["exclude_white_dwarfs"].cast<bool>();
    
    if (params.contains("progress_callback") && !params["progress_callback"].is_none()) {
        core_params.progress_callback = params["progress_callback"].cast<py::function>();
    }
    
    // CORRECTED: Explicitly wrap the py::function in a std::function lambda
    if (params.contains("check_cancelled") && !params["check_cancelled"].is_none()) {
        py::function check_cancelled_py = params["check_cancelled"].cast<py::function>();
        core_params.check_cancelled = [check_cancelled_py]() {
            // This lambda calls the Python function and converts its result to bool
            return check_cancelled_py().cast<bool>();
        };
    }

    // Call the pure C++ function
    std::vector<JumpResult> core_results = run_astar_search_core(core_params);
    
    // Convert the C++ results to a Python list of dictionaries
    py::list final_path;
    if (core_results.empty()) {
        return final_path;
    }

    sqlite3* db;
    sqlite3_open_v2("starmap.db", &db, SQLITE_OPEN_READONLY, nullptr);

    for (size_t i = 0; i < core_results.size(); ++i) {
        const auto& core_step = core_results[i];
        py::dict step;
        step["system_id"] = core_step.system_id;
        step["system_name"] = get_system_name_by_id(db, core_step.system_id);
        step["jump_number"] = i;
        step["jump_distance"] = core_step.jump_distance;
        step["fuel_used"] = core_step.fuel_used;
        step["fuel_remaining"] = core_step.fuel_remaining;
        step["is_refuel_stop"] = core_step.is_refuel_stop;
        
        py::object boost_type = py::none();
        if (core_step.boost_type == 1) boost_type = py::cast("Neutron Star");
        if (core_step.boost_type == 2) boost_type = py::cast("White Dwarf");
        step["boost_type"] = boost_type;

        step["warnings"] = core_step.warnings;
        final_path.append(step);
    }
    
    sqlite3_close(db);
    return final_path;
}


PYBIND11_MODULE(astar_cpp, m) {
    m.doc() = "High-performance A* pathfinding for Elite Dangerous (Python Wrapper)";
    m.def("initialize_in_memory_data", &initialize_in_memory_data, "Loads essential star data into memory from a binary cache.");
    m.def("run_astar_search", &run_astar_search_wrapper, "Runs the bi-directional, fuel-aware A* search from memory.");
}

