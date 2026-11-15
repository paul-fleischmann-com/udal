# googleapis (vendored, Apache-2.0)

`google/api/annotations.proto` and `google/api/http.proto` from https://github.com/googleapis/googleapis,
unchanged. They replace the `buf.build/googleapis/googleapis` dependency for **offline** generation
(`buf.local.yaml`), where no network is available (the Software Factory regenerates code in a container
without network). The normal workflow (`buf.yaml`, `make generate`) is unchanged and keeps using the dependency.
