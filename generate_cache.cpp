#include <iostream>
#include <fstream>
#include <vector>
#include <string>
#include <unordered_map>
#include <unordered_set>
#include <chrono>
#include <cstdint>
#include <sqlite3.h>

// Boost Serialization headers
#include <boost/archive/binary_oarchive.hpp>
#include <boost/serialization/vector.hpp>
#include <boost/serialization/unordered_map.hpp>
#include <boost/serialization/string.hpp>
#include <boost/serialization/utility.hpp> // For std::pair
#include <boost/serialization/split_free.hpp> // For split_free

// Boost Geometry headers
#include <boost/geometry.hpp>
#include <boost/geometry/geometries/point.hpp>
#include <boost/geometry/geometries/box.hpp>
#include <boost/geometry/index/rtree.hpp>

namespace bg = boost::geometry;
namespace bgi = boost::geometry::index;

// --- Data Structures ---

using point = bg::model::point<double, 3, bg::cs::cartesian>;
using value = std::pair<point, long long>;

struct Coords {
    double x, y, z;
    template<class Archive>
    void serialize(Archive & ar, const unsigned int version) {
        ar & x & y & z;
    }
};

struct StarInfo {
    uint8_t flags;
    double distance_ls;
    template<class Archive>
    void serialize(Archive & ar, const unsigned int version) {
        ar & flags & distance_ls;
    }
};

struct SystemCacheInfo {
    Coords coords;
    std::vector<StarInfo> stars;
    template<class Archive>
    void serialize(Archive & ar, const unsigned int version) {
        ar & coords & stars;
    }
};

namespace boost { namespace serialization {
template<class Archive>
void save(Archive & ar, const point & p, const unsigned int version) {
    ar & bg::get<0>(p);
    ar & bg::get<1>(p);
    ar & bg::get<2>(p);
}
template<class Archive>
void load(Archive & ar, point & p, const unsigned int version) {
    double x, y, z;
    ar & x & y & z;
    bg::set<0>(p, x);
    bg::set<1>(p, y);
    bg::set<2>(p, z);
}
template<class Archive>
void serialize(Archive & ar, point & p, const unsigned int version) {
    boost::serialization::split_free(ar, p, version);
}
}}

// --- Main Cache Generation Logic ---

int main() {
    std::cout << "Starting binary cache generation..." << std::endl;
    auto start_time = std::chrono::steady_clock::now();

    std::unordered_map<long long, SystemCacheInfo> system_cache;

    sqlite3* db;
    if (sqlite3_open_v2("starmap.db", &db, SQLITE_OPEN_READONLY, nullptr) != SQLITE_OK) {
        std::cerr << "Error: Cannot open starmap.db" << std::endl;
        return 1;
    }
    std::cout << "Database opened successfully." << std::endl;

    std::cout << "Loading system coordinates..." << std::endl;
    sqlite3_stmt* system_stmt;
    const char* system_query = "SELECT id, x, y, z FROM systems;";
    sqlite3_prepare_v2(db, system_query, -1, &system_stmt, nullptr);
    
    std::vector<value> systems_to_bulk_load;
    while (sqlite3_step(system_stmt) == SQLITE_ROW) {
        long long id = sqlite3_column_int64(system_stmt, 0);
        Coords coords = {
            sqlite3_column_double(system_stmt, 1),
            sqlite3_column_double(system_stmt, 2),
            sqlite3_column_double(system_stmt, 3)
        };
        system_cache[id].coords = coords;
        point p(coords.x, coords.y, coords.z);
        systems_to_bulk_load.push_back(std::make_pair(p, id));
    }
    sqlite3_finalize(system_stmt);
    std::cout << "Loaded " << system_cache.size() << " systems." << std::endl;

    std::cout << "Loading and processing stars..." << std::endl;
    sqlite3_stmt* star_stmt;
    const char* star_query = "SELECT system_id, sub_type, distance_ls FROM stars;";
    sqlite3_prepare_v2(db, star_query, -1, &star_stmt, nullptr);
    
    long long star_count = 0;
    long long matched_star_count = 0;
    long long scoopable_matched = 0;
    long long neutron_matched = 0;
    long long wd_matched = 0;

    while (sqlite3_step(star_stmt) == SQLITE_ROW) {
        long long system_id = sqlite3_column_int64(star_stmt, 0);
        
        std::string sub_type_text;
        if (sqlite3_column_type(star_stmt, 1) == SQLITE_TEXT) {
            const unsigned char* sub_type_unsigned_c = sqlite3_column_text(star_stmt, 1);
            if (sub_type_unsigned_c) {
                sub_type_text = reinterpret_cast<const char*>(sub_type_unsigned_c);
            }
        }
        
        uint8_t flags = 0;
        bool is_scoopable = false;
        bool is_neutron = false;
        bool is_wd = false;

        if (!sub_type_text.empty()) {
            auto starts_with = [&](const std::string& prefix) {
                return sub_type_text.rfind(prefix, 0) == 0;
            };

            if (starts_with("A ") || starts_with("B ") || starts_with("F ") || starts_with("G ") || 
                starts_with("K ") || starts_with("M ") || starts_with("O ")) {
                flags |= (1 << 0);
                is_scoopable = true;
            }

            if (sub_type_text.find("Neutron Star") != std::string::npos) {
                flags |= (1 << 1);
                is_neutron = true;
            }
            if (sub_type_text.find("White Dwarf") != std::string::npos) {
                flags |= (1 << 2);
                is_wd = true;
            }
        }

        if (flags > 0) {
            matched_star_count++;
            if (is_scoopable) scoopable_matched++;
            if (is_neutron) neutron_matched++;
            if (is_wd) wd_matched++;
        }

        system_cache[system_id].stars.push_back({flags, sqlite3_column_double(star_stmt, 2)});
        star_count++;

        if (star_count % 500000 == 0) {
             std::cout << "[CACHE GEN DEBUG] Processed " << star_count << " stars..."
                      << " | Scoopable: " << scoopable_matched 
                      << ", Neutron: " << neutron_matched 
                      << ", WD: " << wd_matched 
                      << " | Last subtype seen: '" << sub_type_text << "'" << std::endl;
        }
    }
    sqlite3_finalize(star_stmt);
    sqlite3_close(db);
    std::cout << "Processed " << star_count << " total stars." << std::endl;
    std::cout << "Found " << matched_star_count << " relevant stars total:" << std::endl;
    std::cout << "  - Scoopable: " << scoopable_matched << std::endl;
    std::cout << "  - Neutron:   " << neutron_matched << std::endl;
    std::cout << "  - White Dwarf: " << wd_matched << std::endl;


    std::cout << "Serializing data to starmap.cache..." << std::endl;
    try {
        std::ofstream ofs("starmap.cache", std::ios::binary);
        if (!ofs) {
            throw std::runtime_error("Cannot open starmap.cache for writing.");
        }
        boost::archive::binary_oarchive oa(ofs);
        
        // CORRECTED: Convert the map to a vector for stable serialization
        std::vector<std::pair<long long, SystemCacheInfo>> cache_as_vector(system_cache.begin(), system_cache.end());

        oa << systems_to_bulk_load;
        oa << cache_as_vector; // Serialize the vector, not the map

    } catch (const std::exception& e) {
        std::cerr << "Error during serialization: " << e.what() << std::endl;
        return 1;
    }

    auto end_time = std::chrono::steady_clock::now();
    auto duration = std::chrono::duration_cast<std::chrono::seconds>(end_time - start_time).count();
    std::cout << "Successfully created starmap.cache in " << duration << " seconds." << std::endl;

    return 0;
}

