import sqlite3
import json
import signal
import sys
import threading
from flask import Flask, request, jsonify, Response, render_template
from flask_cors import CORS
import astar_cpp

app = Flask(__name__)
CORS(app)

DB_FILENAME = "starmap.db"

# --- Graceful Shutdown Handling ---
# Use a thread-safe event to signal shutdown
shutdown_event = threading.Event()

def signal_handler(sig, frame):
    """Handles Ctrl+C by setting the shutdown event."""
    print("\n[Python] Ctrl+C detected! Signaling C++ engine to shut down...")
    shutdown_event.set()
    # The C++ module will see this and exit, allowing the main thread to terminate.

# Register the signal handler for SIGINT (Ctrl+C)
signal.signal(signal.SIGINT, signal_handler)
# --- End Shutdown Handling ---


# --- One-Time C++ Module Initialization ---
print("--- Elite Dangerous High-Performance Router ---")
print("Initializing in-memory C++ module...")
astar_cpp.initialize_in_memory_data("starmap.cache")
print("C++ module initialized successfully.")
print("---------------------------------------------")

def get_db_connection():
    """Establishes a connection to the SQLite database for name lookups."""
    conn = sqlite3.connect(f"file:{DB_FILENAME}?mode=ro", uri=True)
    conn.row_factory = sqlite3.Row
    return conn

# --- API Endpoints ---

@app.route('/')
def index():
    return render_template('index.html')

@app.route('/api/search', methods=['GET'])
def search_systems():
    """Searches for systems by name using the FTS table."""
    system_name = request.args.get('name')
    if not system_name:
        return jsonify({"error": "System name parameter is required"}), 400

    conn = get_db_connection()
    query = """
        SELECT s.id, s.name FROM systems_fts f
        JOIN systems s ON f.rowid = s.id
        WHERE f.name MATCH ?
        ORDER BY f.rank
        LIMIT 20
    """
    systems = conn.execute(query, (system_name + '*',)).fetchall()
    conn.close()

    return jsonify([dict(s) for s in systems])


@app.route('/api/plot_route_stream', methods=['GET'])
def plot_route_stream():
    """Uses the high-performance C++ module to calculate the route."""
    try:
        params = {
            "start_id": int(request.args.get('start_id')),
            "end_id": int(request.args.get('end_id')),
            "jump_range": float(request.args.get('jump_range')),
            "fuel_capacity": float(request.args.get('fuel_capacity')),
            "max_star_distance": float(request.args.get('max_star_distance')),
            "use_neutron": request.args.get('use_neutron', 'false').lower() == 'true',
            "exclude_white_dwarfs": request.args.get('exclude_white_dwarfs', 'false').lower() == 'true',
        }
    except (TypeError, ValueError) as e:
        error_response = json.dumps({"error": f"Invalid or missing parameter: {e}"})
        return Response(f"data: {error_response}\n\n", mimetype='text/event-stream')

    def log_progress(message):
        print(message, flush=True)

    # This function will be passed to the C++ module for cancellation checks.
    def check_cancelled():
        return shutdown_event.is_set()

    params['progress_callback'] = log_progress
    params['check_cancelled'] = check_cancelled

    def event_stream():
        try:
            route = astar_cpp.run_astar_search(params)
            
            if not route:
                # Check if the empty route was due to cancellation
                if check_cancelled():
                     error_response = json.dumps({"error": "Route calculation cancelled by user."})
                else:
                     error_response = json.dumps({"error": "Could not find a route."})
                yield f"data: {error_response}\n\n"
                return
                
            final_data = {
                "jumps": len(route) - 1 if route else 0,
                "route": route
            }
            yield f"data: {json.dumps(final_data)}\n\n"

        except Exception as e:
            # Catch exceptions that might be thrown from C++
            print(f"Error during A* search: {e}")
            error_response = json.dumps({"error": str(e)})
            yield f"data: {error_response}\n\n"

    return Response(event_stream(), mimetype='text/event-stream')


if __name__ == '__main__':
    app.run(debug=True, port=5000)

