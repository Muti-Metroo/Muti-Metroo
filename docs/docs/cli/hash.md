---
title: hash
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole generating hash" style={{maxWidth: '180px'}} />
</div>

# muti-metroo hash

Turn a password into a secure hash you can safely put in config files. The hash can't be reversed back to the password, but it can verify when someone enters the correct password.

**Quick usage:**

```bash
# Interactive (recommended - hides your typing)
muti-metroo hash

# The output goes in your config file
# password_hash: "$2a$10$N9qo8uLOickgx2ZMRZoMye..."
```

## Synopsis

```bash
muti-metroo hash [password] [flags]
```

## Description

The `hash` command generates bcrypt password hashes that can be used in Muti Metroo configuration files for authentication. Bcrypt is a secure, one-way hashing algorithm specifically designed for passwords.

The generated hash can be used in:

| Config Field                        | Purpose                              |
| ----------------------------------- | ------------------------------------ |
| `http.token_hash`                   | HTTP API bearer token authentication |
| `socks5.auth.users[].password_hash` | SOCKS5 proxy authentication          |

Management commands, file transfer, sleep/wake, and dynamic config mutations are authorized by the mesh management Ed25519 signing keypair, not bcrypt passwords. See [Authentication](/security/authentication).

## Usage

### Interactive Mode (Recommended)

For security, use interactive mode which hides your password input:

```bash
muti-metroo hash
```

You will be prompted to enter and confirm your password:

```
Enter password:
Confirm password:
$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy
```

### Command Line Argument

You can also provide the password as an argument, but this is less secure as the password may be visible in shell history:

```bash
muti-metroo hash "mysecretpassword"
```

Output:

```
$2a$10$eXaMpLeHaShThAtIsUnIqUeAnDlOnGeNoUgHtObE
```

## Flags

| Flag         | Default | Description                                           |
| ------------ | ------- | ----------------------------------------------------- |
| `--cost`     | 12      | bcrypt cost factor (4-31; default follows OWASP 2024) |
| `-h, --help` |         | Show help                                             |

## Cost Factor

The cost factor determines how computationally expensive the hash is to generate and verify. Higher cost = more secure but slower.

| Cost | Time (approx) | Recommendation             |
| ---- | ------------- | -------------------------- |
| 10   | ~100ms        | Development/testing        |
| 12   | ~400ms        | Production (recommended)   |
| 14   | ~1.5s         | High security environments |

Cost 12 is the default, so a plain `muti-metroo hash` already produces a
production-strength hash. Override only to go higher (or lower for testing):

```bash
muti-metroo hash --cost 14
```

## Examples

### Generate Hash for SOCKS5 User

```bash
# Generate hash
muti-metroo hash --cost 12
Enter password:
Confirm password:
$2a$12$xYzAbCdEfGhIjKlMnOpQrStUvWxYz0123456789AbCdEfGhI

# Use in config.yaml
# socks5:
#   auth:
#     enabled: true
#     users:
#       - username: "admin"
#         password_hash: "$2a$12$xYzAbCdEfGhIjKlMnOpQrStUvWxYz0123456789AbCdEfGhI"
```

### Generate Hash for the HTTP API Bearer Token

```bash
muti-metroo hash --cost 12

# Use in config.yaml
# http:
#   enabled: true
#   token_hash: "$2a$12$..."
```

### Using Environment Variables

You can store the hash in an environment variable and reference it in config:

```bash
# Generate and export
export SOCKS5_PASSWORD_HASH=$(muti-metroo hash "mypassword")

# In config.yaml
# socks5:
#   auth:
#     users:
#       - username: "admin"
#         password_hash: "${SOCKS5_PASSWORD_HASH}"
```

### Scripting

For automation, pass the password as an argument:

```bash
# From a file (avoids shell history)
muti-metroo hash "$(cat /path/to/password/file)"

# From an environment variable
muti-metroo hash "$MY_PASSWORD"
```

:::note
Piping via stdin (e.g., `echo "pw" | muti-metroo hash`) does not work -- the interactive prompt requires a terminal. Pass the password as a command-line argument instead.
:::

## Hash Format

The generated hash follows the bcrypt format:

```
$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy
|  |  |                                                      |
|  |  +-- 22-character salt + 31-character hash              |
|  +-- cost factor (10 = 2^10 iterations)                    |
+-- bcrypt algorithm identifier                               |
```

- `$2a$` - bcrypt algorithm version
- `10$` - cost factor (10 means 2^10 = 1024 iterations)
- Remaining characters - salt (22 chars) + hash (31 chars)

## Security Considerations

1. **Use interactive mode**: Avoid putting passwords in command line arguments when possible, as they may be logged in shell history.

2. **Use appropriate cost**: Balance security with performance. Cost 12 is recommended for production.

3. **Unique passwords**: Use different passwords for the SOCKS5 user list and the HTTP API bearer token.

4. **Store hashes safely**: Even though hashes are one-way, treat configuration files with hashes as sensitive.

5. **Rotate passwords**: Periodically generate new hashes and update configurations.

## Alternative Methods

If you cannot use the Muti Metroo CLI, you can generate bcrypt hashes using:

### htpasswd (Apache)

```bash
htpasswd -bnBC 10 "" yourpassword | tr -d ':\n'
```

### Python

```python
import bcrypt
print(bcrypt.hashpw(b"yourpassword", bcrypt.gensalt(10)).decode())
```

### Node.js

```javascript
const bcrypt = require("bcrypt");
console.log(bcrypt.hashSync("yourpassword", 10));
```

### Go

```go
import "golang.org/x/crypto/bcrypt"
hash, _ := bcrypt.GenerateFromPassword([]byte("yourpassword"), 10)
fmt.Println(string(hash))
```

## Related

- [Authentication](/security/authentication) - Complete authentication guide
- [SOCKS5 Configuration](/configuration/socks5) - SOCKS5 proxy settings
- [HTTP API](/configuration/http) - HTTP API bearer token configuration
