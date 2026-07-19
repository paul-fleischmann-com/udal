# udal-sdk

Python client SDK for [UDAL](https://github.com/paul-fleischmann-com/udal) (Universal Device
Abstraction Layer) — asyncio-based, application and device side (req42.adoc §7.3).

```python
import asyncio
from udal import Client, ClientConfig

async def main() -> None:
    async with Client(ClientConfig(gateway_url="localhost:50051", api_key="...")) as client:
        value = await client.get_property("dev-1", "temperature")
        await client.write_property("dev-1", "setpoint", 21.5)
        async for update in client.subscribe("dev-1"):
            print(update)

asyncio.run(main())
```

Device side:

```python
import asyncio
from udal import Device, DeviceConfig

async def main() -> None:
    device = Device(DeviceConfig(gateway_url="localhost:50051", name="sensor-1", capability="temperature-sensor"))

    def reboot(params: dict) -> None:
        print("rebooting", params)

    device.on_command("reboot", reboot)
    await device.run()

asyncio.run(main())
```

See `docs/req42/req42.adoc` §7.3 for the full operation contract shared with the Go, Rust, and
TypeScript SDKs.
