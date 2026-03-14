<div align="center">

# pierx

Tunnel direct. No relay. No limits.

![pierx Demo](pierx.gif)

[![Go](https://img.shields.io/badge/go-1.24+-00ADD8.svg?logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Stars](https://img.shields.io/github/stars/pajarori/pierx?style=flat&logo=github)](https://github.com/pajarori/pierx/stargazers)
[![Forks](https://img.shields.io/github/forks/pajarori/pierx?style=flat&logo=github)](https://github.com/pajarori/pierx/network/members)
[![Issues](https://img.shields.io/github/issues/pajarori/pierx?style=flat&logo=github)](https://github.com/pajarori/pierx/issues)
[![Last Commit](https://img.shields.io/github/last-commit/pajarori/pierx?style=flat&logo=github)](https://github.com/pajarori/pierx/commits/main)

</div>

## Installation

```bash
go install github.com/pajarori/pierx@latest
```

or download from [releases](https://github.com/pajarori/pierx/releases).

## Usage

```bash
# Expose a local HTTP app
pierx http 3000

# Expose a local TCP service like SSH
pierx tcp 22

# Restrict allowed source IPs for TCP tunnels
pierx tcp 22 --allow 1.2.3.4/32 --allow 10.0.0.0/8
```

## Options

| Flag | Description |
|---|---|
| `--port` | Local port to expose |
| `--allow` | Allowed source IPs or CIDRs for TCP tunnels |

## License
 
MIT License