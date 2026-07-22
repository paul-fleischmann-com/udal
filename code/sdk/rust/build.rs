// Compiles the shared `udal.v1` protobuf/gRPC definitions (source of truth:
// code/api/proto/udal/v1/) into Rust via tonic-build, mirroring how the Go
// SDK consumes buf-generated stubs and the Python SDK consumes
// grpcio-tools-generated ones.
//
// Only runs when the "std" feature is enabled: the `mqtt` (no_std) build
// never touches gRPC at all, and requiring protoc/tonic-build for a
// bare-metal target build would be both wrong (no gRPC there) and slow.
//
// Uses a vendored protoc (`protoc-bin-vendored`) rather than requiring a
// system install, and a small local vendor copy of the well-known-type and
// `google.api.http` annotation `.proto` files (third_party/) rather than
// fetching them from the network at build time — CI has no step that
// installs protoc for the Rust job, and the annotation options used by
// device.proto/capability.proto (for the gRPC-gateway REST transcoding,
// unused by this SDK) aren't resolvable without them physically present.
fn main() {
    if std::env::var_os("CARGO_FEATURE_STD").is_none() {
        return;
    }

    let protoc = protoc_bin_vendored::protoc_bin_path().expect("vendored protoc binary");
    // SAFETY: build scripts are single-threaded at this point in the build
    // graph — no concurrent access to the process environment.
    unsafe {
        std::env::set_var("PROTOC", protoc);
    }

    let proto_root = "../../api/proto";
    let files = [
        format!("{proto_root}/udal/v1/device.proto"),
        format!("{proto_root}/udal/v1/capability.proto"),
    ];
    for f in &files {
        println!("cargo:rerun-if-changed={f}");
    }

    tonic_prost_build::configure()
        .build_server(false)
        .extern_path(".google.protobuf.Struct", "::prost_types::Struct")
        .extern_path(".google.protobuf.Value", "::prost_types::Value")
        .extern_path(".google.protobuf.Timestamp", "::prost_types::Timestamp")
        .compile_protos(&files, &[proto_root.to_string(), "third_party".to_string()])
        .expect("compile udal.v1 protobuf/gRPC definitions");
}
