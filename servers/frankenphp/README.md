# ESI middleware for FrankenPHP
A lightweight implementation of Edge Side Includes (ESI) middleware for FrankenPHP server

## Building FrankenPHP with mESI middleware
To add the mesi middleware to the FrankenPHP server, you need to compile it properly. 
The best way to do this is to use the [xcaddy compiler](https://github.com/caddyserver/xcaddy)

```shell
go install github.com/caddyserver/xcaddy/cmd/xcaddy@latest
```

Then just run the command below
```shell
CGO_ENABLED=1 \
XCADDY_GO_BUILD_FLAGS="-ldflags='-w -s' -tags=nobadger,nomysql,nopgx,nowatcher" \
CGO_CFLAGS=$$(php-config --includes) \
CGO_LDFLAGS="$$(php-config --ldflags) $$(php-config --libs)" \
xcaddy build \
    --output frankenphp \
    --with github.com/dunglas/frankenphp \
    --with github.com/crazy-goat/go-mesi/servers/caddy=../caddy
```

Then we can check if caddy contains the right module using this command

```shell
frankenphp list-modules | grep mesi
```

this command should return 

```
http.handlers.mesi
```

## Configuration
Then you need to disable the mESSI middleware for that server.
You also need to set the appropriate order of the handlers using the order directive.

```
{
    order mesi before file_server
}

:8080 {
    root * ../../examples
    mesi
    file_server
}
```

Finally, you can start the FrankenPHP server with the command:

```shell
frankenphp run --config Caddyfile
```

## Testing

Integration tests verify ESI processing for both static HTML and PHP-generated content:

```shell
make test
```

This uses `docker compose` to build a FrankenPHP image with the mesi module, starts a test backend server, and runs curl-based tests against the following scenarios:

- Module loaded: `http.handlers.mesi` registered
- ESI include in static HTML and PHP-generated HTML
- ESI comment unwrapping (`<!--esi ... -->`)
- ESI remove (`<esi:remove>`)
- Surrogate-Capability header presence
- Non-HTML content bypass (text/plain, application/json)
- Content-Length correctness
- Content-Type preservation