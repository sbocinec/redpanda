find_package(Hdrhistogram)
find_package(Base64)
v_cc_library(
  NAME utils
  SRCS
    hdr_hist.cc
    human.cc
    file_io.cc
    base64.cc
    retry_chain_node.cc
    tracking_allocator.cc
    vint.cc
    directory_walker.cc
    uuid.cc
    request_auth.cc
    bottomless_token_bucket.cc
    utf8.cc
  DEPS
    Seastar::seastar
    Hdrhistogram::hdr_histogram
    aklomp::base64
    absl::hash
    absl::random_seed_sequences
    v::bytes
    v::rphashing
    v::json)
add_subdirectory(tests)
