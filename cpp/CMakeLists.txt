cmake_minimum_required(VERSION 3.20)
project(orbit_cpp LANGUAGES CXX)

set(CMAKE_CXX_EXTENSIONS OFF)

find_package(Threads REQUIRED)
add_library(orbit_cpp STATIC src/rpc.cpp src/tcp_transport.cpp src/tcp_client.cpp
  src/request_server.cpp)
set_target_properties(orbit_cpp PROPERTIES POSITION_INDEPENDENT_CODE ON)
add_library(orbit::cpp ALIAS orbit_cpp)
target_compile_features(orbit_cpp PUBLIC cxx_std_23)
target_include_directories(orbit_cpp PUBLIC "${CMAKE_CURRENT_SOURCE_DIR}/include")
target_link_libraries(orbit_cpp PRIVATE Threads::Threads)
if(WIN32)
  target_link_libraries(orbit_cpp PRIVATE ws2_32)
endif()

if(CMAKE_SOURCE_DIR STREQUAL CMAKE_CURRENT_SOURCE_DIR)
  add_executable(orbit_cpp_echo ../examples/cpp-client/main.cpp)
  target_link_libraries(orbit_cpp_echo PRIVATE orbit::cpp)
  add_executable(orbit_cpp_request_server ../examples/cpp-server/main.cpp)
  target_link_libraries(orbit_cpp_request_server PRIVATE orbit::cpp)

  include(CTest)
  if(BUILD_TESTING)
    add_executable(orbit_cpp_rpc_test tests/rpc_test.cpp)
    target_link_libraries(orbit_cpp_rpc_test PRIVATE orbit::cpp)
    add_test(NAME orbit_cpp_rpc COMMAND orbit_cpp_rpc_test)
    add_executable(orbit_cpp_request_server_test tests/request_server_test.cpp)
    target_link_libraries(orbit_cpp_request_server_test PRIVATE orbit::cpp)
    add_test(NAME orbit_cpp_request_server COMMAND orbit_cpp_request_server_test)
    set_tests_properties(orbit_cpp_request_server PROPERTIES TIMEOUT 15)

    if(CMAKE_SYSTEM_NAME STREQUAL "Linux")
      add_library(orbit_cpp_shared_consumer SHARED tests/shared_consumer.cpp)
      target_link_libraries(orbit_cpp_shared_consumer PRIVATE orbit::cpp)
    endif()

    option(ORBIT_CPP_TEST_GO_INTEROP "Test Go and C++ TCP interoperability" OFF)
    if(ORBIT_CPP_TEST_GO_INTEROP)
      find_program(ORBIT_GO_EXECUTABLE go REQUIRED)
      add_test(NAME orbit_cpp_go_interop
        COMMAND "${CMAKE_COMMAND}" -E env "ORBIT_CPP_ECHO_CLIENT=$<TARGET_FILE:orbit_cpp_echo>"
          "${ORBIT_GO_EXECUTABLE}" test -tags=cppinterop -count=1 -timeout=30s
          -run=^TestCPPEcho$ ./examples/client
        WORKING_DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}/.."
      )
      add_executable(orbit_cpp_request_fixture tests/request_server_fixture.cpp)
      target_link_libraries(orbit_cpp_request_fixture PRIVATE orbit::cpp)
      add_test(NAME orbit_go_cpp_request_interop
        COMMAND "${CMAKE_COMMAND}" -E env "ORBIT_CPP_REQUEST_SERVER=$<TARGET_FILE:orbit_cpp_request_fixture>"
          "${ORBIT_GO_EXECUTABLE}" test -tags=cppinterop -count=1 -timeout=60s
          -run=^TestCPPRequestServer$ ./examples/client
        WORKING_DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}/.."
      )
    endif()
  endif()
endif()
