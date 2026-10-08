# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| 0.x (pre-release) | ✅ Current development branch |

Once v1.0.0 is released, only the latest minor release will receive security fixes.

## Reporting a Vulnerability

**Please do not report security vulnerabilities via public GitHub Issues.**

Send a detailed report to: **paul.fleischmann81@gmail.com**

Include in your report:
- Description of the vulnerability and its potential impact
- Steps to reproduce or a proof-of-concept (if available)
- Affected component(s): gateway, SDK (Go/Rust/Python/TypeScript), transport adapter
- Any suggested mitigations

### What to expect

| Timeline | Action |
|---|---|
| Within 48 hours | Acknowledgement of your report |
| Within 7 days | Initial assessment and severity classification |
| Within 90 days | Patch release or workaround (depending on severity) |

We follow a **coordinated disclosure** model: we ask that you give us reasonable time to address the issue before public disclosure. In return, we will credit you in the release notes unless you prefer to remain anonymous.

## Scope

The following are in scope:

- `udal-gateway` binary and Docker image
- Go, Rust, Python, TypeScript SDKs
- Transport adapters (MQTT, HTTP, CAN)
- Authentication and authorization (API-key, JWT, mTLS, RBAC)
- Protobuf/gRPC API surface

The following are **out of scope**:

- Third-party dependencies (report those to the respective upstream projects)
- Issues requiring physical access to the deployment environment
- Social engineering attacks

## Security Considerations for Operators

- Run the gateway with the principle of least privilege
- Enable mTLS for production deployments
- Rotate API keys and JWT signing keys regularly
- Keep the gateway and SDK versions up to date — subscribe to [GitHub Security Advisories](https://github.com/paul-fleischmann-com/udal/security/advisories)
- Review the SBOM attached to each release (`udal-gateway_*_sbom.json`) for dependency inventory

## PGP Key

No PGP key is currently published. Plain email to the address above is sufficient.
