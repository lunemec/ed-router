from setuptools import setup, Extension
import sys
import os
import pybind11

# --- Configuration ---
BOOST_ROOT = os.environ.get("BOOST_ROOT")
SQLITE_ROOT = os.environ.get("SQLITE_ROOT")

# --- Platform-specific compiler arguments and library settings ---
extra_compile_args = []
extra_link_args = []
libraries = []
include_dirs = [pybind11.get_include()]
library_dirs = []

# Define the sources for our pure C++ core library
core_sources = ['astar_core.cpp']

if sys.platform == 'win32':
    extra_compile_args = ['/EHsc', '/O2', '/std:c++17', '/MT']
    
    if not BOOST_ROOT:
        raise ValueError("BOOST_ROOT environment variable is not set.")
    if not SQLITE_ROOT:
        raise ValueError("SQLITE_ROOT environment variable is not set.")
    
    include_dirs.append(BOOST_ROOT)
    include_dirs.append(SQLITE_ROOT)
    # The sqlite source is now part of the core library
    core_sources.append(os.path.join(SQLITE_ROOT, 'sqlite3.c'))
    library_dirs.append(os.path.join(BOOST_ROOT, 'stage', 'lib'))
    # The python extension will link against the boost serialization static lib
    libraries.append('libboost_serialization-vc143-mt-s-x64-1_89')

elif sys.platform == 'darwin': # macOS
    extra_compile_args = ['-std=c++17', '-O3']
    libraries = ['sqlite3', 'boost_serialization']
    if os.path.exists('/usr/local/include'):
         include_dirs.append('/usr/local/include')
    if os.path.exists('/opt/homebrew/include'):
         include_dirs.append('/opt/homebrew/include')
    if os.path.exists('/opt/homebrew/lib'):
         library_dirs.append('/opt/homebrew/lib')

else: # Linux
    extra_compile_args = ['-std=c++17', '-O3']
    libraries = ['sqlite3', 'boost_serialization']

# Define the C++ extension module
astar_cpp_module = Extension(
    'astar_cpp',
    sources=['astar_wrapper.cpp'] + core_sources, # Compile all sources together
    include_dirs=include_dirs,
    library_dirs=library_dirs,
    libraries=libraries,
    extra_compile_args=extra_compile_args,
    extra_link_args=extra_link_args,
    language='c++'
)

setup(
    name='astar_cpp',
    version='4.0',
    description='High-performance A* pathfinding module with pure C++ core',
    ext_modules=[astar_cpp_module],
)

