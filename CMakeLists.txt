cmake_minimum_required( VERSION 3.1 )
project( audiolink VERSION 0.0.1 )

find_package( Argumentum CONFIG REQUIRED )
set( CMAKE_CXX_STANDARD 17 )

add_executable( audiolink
   AudioLink.cpp
   )

target_link_libraries( audiolink
   PRIVATE
   Argumentum::argumentum
   )