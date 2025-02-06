# ESI middleware for traefik
A lightweight implementation of Edge Side Includes (ESI) middleware for Traefik

## Installation

Add `mesi` plugin in main `traefik.yaml` configuration file
```yaml
experimental:
  plugins:
    mesi:
      modulename: https://github.com/crazy-goat/go-mesi
      version: v0.1
```

## Configuration

Add `mesi` plugin to http middleware and add it to specific server:
```yaml
http:
  middlewares:
    mesi:
      plugin:
        mesi:
          maxDepth: 5

  routers:
    test-server:
      middlewares:
        - mesi
      service: test-server
      # more config here

  services:
    test-server:
    # some service config here
```