# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in `go-flightrecorder`, please
report it responsibly.

**Do not open a public GitHub issue.**

Instead, use GitHub's private vulnerability reporting:

**<https://github.com/LarsArtmann/go-flightrecorder/security/advisories/new>**

This creates a private advisory visible only to the repository maintainers,
and lets us coordinate a fix before any public disclosure.

## Response Timeline

| Step                     | Target   |
| ------------------------ | -------- |
| Acknowledgment of report | 48 hours |
| Initial assessment       | 5 days   |
| Fix or mitigation        | 30 days  |

If you have not received a response within 48 hours, please follow up by
commenting on the private advisory.

## Scope

This is a library wrapping Go's `runtime/trace.FlightRecorder`. It does not
expose a network surface. Vulnerabilities relevant to this project include:

- Panics or data races triggered by valid API usage.
- Unsafe handling of file paths or writer sinks (e.g., path traversal,
  resource leaks).
- Violations of the documented concurrency or lifecycle contracts.

Reports about the underlying Go runtime belong in the
[Go project's security policy](https://go.dev/security), not here.

## Supported Versions

Only the latest released version receives security fixes.

| Version | Supported |
| ------- | --------- |
| latest  | ✅        |
| older   | ❌        |
