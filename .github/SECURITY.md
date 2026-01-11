# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 1.x.x   | :white_check_mark: |
| < 1.0   | :x:                |

## Reporting a Vulnerability

If you discover a security vulnerability, please report it privately:

1. **Do not** open a public GitHub issue
2. Email the maintainers directly or use GitHub's private vulnerability reporting feature
3. Include:
   - Description of the vulnerability
   - Steps to reproduce
   - Potential impact
   - Any suggested fixes

We will acknowledge receipt within 48 hours and provide a detailed response within 7 days.

## Security Considerations

This operator runs privileged containers for:
- **Driver Agent**: Loads kernel modules, requires host access
- **Device Plugin**: Accesses `/dev/cxi*` devices
- **Retry Handler**: Requires CAP_SYS_ADMIN for memory operations

Ensure you:
- Only deploy on trusted nodes
- Use RBAC to restrict who can create CXIDriver resources
- Review node selectors to limit deployment scope
