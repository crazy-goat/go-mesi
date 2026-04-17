# Apache HTTP Server mESI Module

Apache output filter module for mESI (Edge Side Includes) processing.

## Requirements

- Apache HTTP Server 2.4+
- libgomesi.so (built from libgomesi/)
- APR, APR-util

## Building

```bash
# Build libgomesi first
cd ../../libgomesi
go build -buildmode=c-shared -o libgomesi.so libgomesi.go
sudo cp libgomesi.so /usr/lib/

# Build Apache module
./build.sh
```

## Installation

```bash
sudo make install
```

## Configuration

```apache
LoadModule mesi_module modules/mod_mesi.so

<Location /esi>
    MesiEnable on
</Location>

# Optional: custom libgomesi path
MesiLibPath /opt/libgomesi.so
```

### Directives

- `MesiEnable on|off` - Enable/disable ESI processing (default: off)
- `MesiLibPath /path/to/libgomesi.so` - Path to libgomesi library (default: /usr/lib/libgomesi.so)

## Testing

```bash
docker-compose up --build
./test.sh
```

## MPM Compatibility

| MPM | Status | Notes |
|-----|--------|-------|
| Prefork | ✅ Recommended | No threading issues |
| Worker | ⚠️ Supported | Mutex around Parse() |
| Event | ⚠️ Supported | Same as Worker |

## How It Works

1. Registers as output filter in Apache's filter chain
2. Intercepts responses with `Content-Type: text/html`
3. Adds `Surrogate-Capability: ESI/1.0` header
4. Buffers response body until complete
5. Processes through libgomesi.Parse()
6. Returns processed HTML to client
