#include <iostream>
#include <vector>
#include <chrono>
#include <sqlite3.h>

// Include Boost Geometry headers for the R-Tree
#include <boost/geometry.hpp>
#include <boost/geometry/geometries/point.hpp>
#include <boost/geometry/index/rtree.hpp>

namespace bg = boost::geometry;
namespace bgi = boost::geometry::index;

// Define a 3D point for our star coordinates
using point = bg::model::point<double, 3, bg::cs::cartesian>;
// Define the value that will be stored in the R-Tree: a pair of a point and the system ID
using value = std::pair<point, long long>;

void check_sqlite_error(int rc, const std::string& msg) {
    if (rc != SQLITE_OK) {
        throw std::runtime_error(msg + ": " + sqlite3_errstr(rc));
    }
}

int main() {
    const char* db_path = "starmap.db";
    sqlite3* db;

    std::cout << "--- In-Memory R-Tree Benchmark ---" << std::endl;

    // --- 1. Open Database ---
    auto start_db_open = std::chrono::high_resolution_clock::now();
    int rc = sqlite3_open_v2(db_path, &db, SQLITE_OPEN_READONLY, nullptr);
    if (rc != SQLITE_OK) {
        std::cerr << "Cannot open database: " << sqlite3_errmsg(db) << std::endl;
        return 1;
    }
    auto end_db_open = std::chrono::high_resolution_clock::now();
    std::chrono::duration<double> db_open_duration = end_db_open - start_db_open;
    std::cout << "Database opened successfully in " << db_open_duration.count() << " seconds." << std::endl;

    // --- 2. Load Data and Build R-Tree ---
    std::cout << "\nStarting to load systems and build R-Tree in memory..." << std::endl;
    std::cout << "Please monitor this process's memory usage in your OS (Task Manager, top, etc.)." << std::endl;

    auto start_build = std::chrono::high_resolution_clock::now();

    // The R-Tree object. This is what will consume the memory.
    // We use quadratic splitting algorithm with a max of 16 elements per node.
    bgi::rtree<value, bgi::quadratic<16>> rtree;

    sqlite3_stmt* stmt;
    const char* query = "SELECT id, x, y, z FROM systems;";
    rc = sqlite3_prepare_v2(db, query, -1, &stmt, nullptr);
    if (rc != SQLITE_OK) {
        std::cerr << "Failed to prepare select statement: " << sqlite3_errmsg(db) << std::endl;
        sqlite3_close(db);
        return 1;
    }

    long long count = 0;
    while (sqlite3_step(stmt) == SQLITE_ROW) {
        long long id = sqlite3_column_int64(stmt, 0);
        double x = sqlite3_column_double(stmt, 1);
        double y = sqlite3_column_double(stmt, 2);
        double z = sqlite3_column_double(stmt, 3);
        
        // Create a point and insert it into the R-Tree
        point p(x, y, z);
        rtree.insert(std::make_pair(p, id));
        
        count++;
        if (count % 1000000 == 0) {
            std::cout << "  - Loaded " << count / 1000000 << " million systems..." << std::endl;
        }
    }

    auto end_build = std::chrono::high_resolution_clock::now();
    std::chrono::duration<double> build_duration = end_build - start_build;

    // --- 3. Report Results ---
    std::cout << "\n--- Benchmark Complete ---" << std::endl;
    std::cout << "Total systems loaded into R-Tree: " << count << std::endl;
    std::cout << "Time to load data and build R-Tree: " << build_duration.count() << " seconds." << std::endl;
    std::cout << "\nTo get the memory usage, please check the peak memory this process consumed in your operating system's activity monitor." << std::endl;

    // --- Cleanup ---
    sqlite3_finalize(stmt);
    sqlite3_close(db);

    return 0;
}
