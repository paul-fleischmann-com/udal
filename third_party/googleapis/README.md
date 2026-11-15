# googleapis (vendored, Apache-2.0)

`google/api/annotations.proto` and `google/api/http.proto` from https://github.com/googleapis/googleapis,
unchanged. They replace the `buf.build/googleapis/googleapis` dependency: `buf.yaml` lists this folder as a
second module, so code generation and lint work **without network** (the Software Factory regenerates code
in a container without network, with `make generate-local`, local plugins from `buf.gen.local.yaml`).
