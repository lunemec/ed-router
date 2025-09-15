use actix_cors::Cors;
use actix_files::NamedFile;
use actix_web::{get, post, web, App, HttpResponse, HttpServer, Responder};
use ed_router::astar::{self, SearchParameters};
use log;
use rkyv::{Archived, Deserialize, Infallible};
use rstar::RTree;
use serde::Serialize;
use std::env;
use std::fs;
use std::path::PathBuf;
use std::process;
use std::sync::Arc;

// Use our new shared library structure
use ed_router::cache::{FullCacheData, SearchQuery, SystemPoint, SystemPointExt};
mod db;

// The main application state.
struct AppState {
    db_pool: db::Pool,
    rtree: RTree<SystemPoint>,
    points: &'static [Archived<SystemPoint>],
}

// The enriched route information sent to the frontend.
#[derive(Serialize)]
struct ApiJumpResult {
    jump_number: usize,
    system_id: i64,
    system_name: String,
    jump_distance: f64,
    fuel_used: f64,
    fuel_remaining: f64,
    boost_type: ed_router::astar::BoostType,
    source_features: Vec<StarFeature>,
    destination_features: Vec<StarFeature>,
    warnings: Vec<String>,
}

#[derive(Serialize)]
pub enum StarFeatureType {
    Scoopable,
    Neutron,
    WhiteDwarf,
}

#[derive(Serialize)]
pub struct StarFeature {
    pub star_type: StarFeatureType,
    pub distance_ls: f32,
}

#[actix_web::main]
async fn main() -> std::io::Result<()> {
    let args: Vec<String> = env::args().collect();
    if args.len() < 3 {
        eprintln!("Error: Missing file path arguments.");
        eprintln!("Usage: {} <cache_path.rkyv> <db_path.sqlite>", args[0]);
        process::exit(1);
    }
    let cache_path = &args[1];
    let db_path = &args[2];

    println!("[Server] Starting server...");
    std::env::set_var("RUST_LOG", "debug");
    env_logger::init();
    let start_time = std::time::Instant::now();

    // --- Step 1 & 2: Load cache via mmap and build R-Tree ---
    println!("[Server] Loading R-Tree cache via memory-map...");
    let file = fs::File::open(cache_path)
        .expect("Failed to read R-Tree cache file. Please run the cache_generator binary first.");
    // Create the mmap and immediately put it on the heap in a Box.
    let mmap = Box::new(unsafe { memmap2::Mmap::map(&file)? });

    // "Leak" the mmap to get a reference that lives for the entire program ('static).
    // This is the key to solving the lifetime issue without copying the data.
    let mmap_static: &'static memmap2::Mmap = Box::leak(mmap);
    let archived_data = unsafe { rkyv::archived_root::<FullCacheData>(&mmap_static[..]) };

    println!(
        "[Server] R-Tree cache file mapped in {:?}.",
        start_time.elapsed()
    );

    println!("[Server] Verifying cache data...");
    println!("[Server] > Total Systems: {}", archived_data.points.len());
    println!(
        "[Server] > Total Scoopable Stars: {}",
        archived_data.stats.scoopable_count
    );
    println!(
        "[Server] > Total Neutron Stars: {}",
        archived_data.stats.neutron_count
    );
    println!(
        "[Server] > Total White Dwarfs: {}",
        archived_data.stats.wd_count
    );

    println!("[Server] Deserializing points for R-Tree...");
    // This is the full deserialization needed for the R-Tree
    let points_for_rtree: Vec<SystemPoint> = archived_data
        .points
        .iter()
        .map(|p| p.deserialize(&mut Infallible).unwrap())
        .collect();

    println!("[Server] Building R-Tree...");
    let rtree = RTree::<SystemPoint>::bulk_load(points_for_rtree);
    println!("[Server] R-Tree built.");

    // --- Step 3: Create the database connection pool ---
    println!("[Server] Creating database connection pool...");
    let db_pool = db::create_pool(db_path);

    // --- Step 4: Share the data with the web server ---
    let app_data = Arc::new(AppState {
        db_pool,
        rtree,
        points: &archived_data.points,
    });
    println!("[Server] Startup complete in {:?}.", start_time.elapsed());
    println!("[Server] Starting HTTP server at http://127.0.0.1:8080");

    HttpServer::new(move || {
        let cors = Cors::permissive();

        App::new()
            .wrap(cors)
            .app_data(web::Data::new(app_data.clone())) // Use Data::new as per Actix docs
            .service(search_systems)
            .service(route_systems)
            .route("/", web::get().to(index))
    })
    .bind(("127.0.0.1", 8080))?
    .run()
    .await
}

async fn index() -> actix_web::Result<NamedFile> {
    let path: PathBuf = "./templates/index.html".parse().unwrap();
    Ok(NamedFile::open(path)?)
}
#[get("/api/search")]
async fn search_systems(
    query: web::Query<SearchQuery>,
    data: web::Data<Arc<AppState>>,
) -> impl Responder {
    let pool = data.db_pool.clone();
    let search_name = query.name.clone();

    let systems_result = web::block(move || db::search_systems_by_name(&pool, &search_name)).await;

    match systems_result {
        Ok(Ok(systems)) => HttpResponse::Ok().json(systems),
        _ => HttpResponse::InternalServerError().json("Error searching for systems"),
    }
}

#[derive(serde::Deserialize)]
pub struct RouteRequest {
    pub start_id: i64,
    pub end_id: i64,
    pub jump_range: f64,
    pub fuel_capacity: f64,
    pub use_neutron: bool,
    pub exclude_white_dwarfs: bool,
    pub max_star_distance: f64,
    pub greediness: f64,
}

#[post("/api/route")]
async fn route_systems(
    params: web::Json<RouteRequest>,
    data: web::Data<Arc<AppState>>,
) -> impl Responder {
    let request_params = params.into_inner();
    let app_data = data.into_inner(); // Get the Arc<AppState>

    // Use web::block to run the potentially long-running search in a separate thread
    let result = web::block(move || -> Result<Option<Vec<ApiJumpResult>>, String> {
        let start_idx = app_data
            .points
            .binary_search_by_key(&request_params.start_id, |p| p.id)
            .map_err(|_| "Start system not found".to_string())?;

        let end_idx = app_data
            .points
            .binary_search_by_key(&request_params.end_id, |p| p.id)
            .map_err(|_| "End system not found".to_string())?;

        let search_params = astar::SearchParameters {
            start_idx,
            end_idx,
            jump_range: request_params.jump_range,
            fuel_capacity: request_params.fuel_capacity,
            use_neutron: request_params.use_neutron,
            exclude_white_dwarfs: request_params.exclude_white_dwarfs,
            max_star_distance: request_params.max_star_distance,
            greediness: request_params.greediness,
        };

        let path = match astar::find_route(&search_params, &app_data.rtree, app_data.points) {
            Some(p) if !p.is_empty() => p,
            _ => return Ok(None),
        };

        let path_ids: Vec<i64> = path.iter().map(|j| j.system_id).collect();
        let system_names =
            db::get_system_names_by_ids(&app_data.db_pool, &path_ids).map_err(|e| e.to_string())?;

        let enriched_path: Vec<ApiJumpResult> = path
            .windows(2)
            .enumerate()
            .map(|(i, jump_pair)| {
                let source_jump = &jump_pair[0];
                let dest_jump = &jump_pair[1];

                let source_system: SystemPoint = app_data.points[source_jump.system_idx]
                    .deserialize(&mut Infallible)
                    .unwrap();
                let dest_system: SystemPoint = app_data.points[dest_jump.system_idx]
                    .deserialize(&mut Infallible)
                    .unwrap();

                ApiJumpResult {
                    jump_number: i + 1,
                    system_id: dest_system.id,
                    system_name: system_names
                        .get(&dest_system.id)
                        .cloned()
                        .unwrap_or_default(),
                    jump_distance: dest_jump.jump_distance,
                    fuel_used: dest_jump.fuel_used,
                    fuel_remaining: dest_jump.fuel_remaining,
                    boost_type: dest_jump.boost_type,
                    source_features: extract_system_features(&source_system, &search_params),
                    destination_features: extract_system_features(&dest_system, &search_params),
                    warnings: dest_jump.warnings.clone(),
                }
            })
            .collect();

        Ok(Some(enriched_path))
    })
    .await;

    match result {
        Ok(Ok(Some(path))) => HttpResponse::Ok().json(path),
        Ok(Ok(None)) => HttpResponse::NotFound().json("No route found."),
        Ok(Err(error_string)) => {
            log::warn!("Client error during pathfinding: {}", error_string);
            HttpResponse::BadRequest().json(error_string)
        }
        Err(e) => {
            log::error!("Internal server error during pathfinding: {:?}", e);
            HttpResponse::InternalServerError()
                .json("An internal error occurred during pathfinding.")
        }
    }
}
fn extract_system_features(system: &SystemPoint, params: &SearchParameters) -> Vec<StarFeature> {
    let mut features = Vec::new();
    if let Some(dist) = system.get_closest_scoopable_ls() {
        if (dist as f64) <= params.max_star_distance {
            features.push(StarFeature {
                star_type: StarFeatureType::Scoopable,
                distance_ls: dist,
            });
        }
    }
    if let Some(dist) = system.get_closest_neutron_ls() {
        if (dist as f64) <= params.max_star_distance {
            features.push(StarFeature {
                star_type: StarFeatureType::Neutron,
                distance_ls: dist,
            });
        }
    }
    if let Some(dist) = system.get_closest_white_dwarf_ls() {
        if (dist as f64) <= params.max_star_distance {
            features.push(StarFeature {
                star_type: StarFeatureType::WhiteDwarf,
                distance_ls: dist,
            });
        }
    }
    features
}
