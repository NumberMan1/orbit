cmake_minimum_required(VERSION 3.20)
project(orbit_cpp LANGUAGES CXX)

set(CMAKE_CXX_EXTENSIONS OFF)

add_library(orbit_cpp STATIC src/rpc.cpp src/tcp_client.cpp)
add_library(orbit::cpp ALIAS orbit_cpp)
target_compile_features(orbit_cpp PUBLIC cxx_std_23)
target_include_directories(orbit_cpp PUBLIC "${CMAKE_CURRENT_SOURCE_DIR}/include")
if(WIN32)
  target_link_libraries(orbit_cpp PRIVATE ws2_32)
endif()

if(CMAKE_SOURCE_DIR STREQUAL CMAKE_CURRENT_SOURCE_DIR)
  add_executable(orbit_cpp_echo ../examples/cpp-client/main.cpp)
  target_link_libraries(orbit_cpp_echo PRIVATE orbit::cpp)

  include(CTest)
  if(BUILD_TESTING)
    add_executable(orbit_cpp_rpc_test tests/rpc_test.cpp)
    target_link_libraries(orbit_cpp_rpc_test PRIVATE orbit::cpp)
    add_test(NAME orbit_cpp_rpc COMMAND orbit_cpp_rpc_test)

    option(ORBIT_CPP_TEST_GO_INTEROP "Test the C++ client against the Go TCP server" OFF)
    if(ORBIT_CPP_TEST_GO_INTEROP)
      find_program(ORBIT_GO_EXECUTABLE go REQUIRED)
      add_test(NAME orbit_cpp_go_interop
        COMMAND "${CMAKE_COMMAND}" -E env "ORBIT_CPP_ECHO_CLIENT=$<TARGET_FILE:orbit_cpp_echo>"
          "${ORBIT_GO_EXECUTABLE}" test -tags=cppinterop -count=1 -timeout=30s
          -run=^TestCPPEcho$ ./examples/client
        WORKING_DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}/.."
      )
    endif()
  endif()
endif()
