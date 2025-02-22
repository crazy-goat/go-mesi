# ESI middleware for Caddy
A lightweight implementation of Edge Side Includes (ESI) middleware for Caddy server

## Building Caddy with mESI middleware
To add the mesi middleware to the Caddy server, you need to compile it properly. 
The best way to do this is to use the [xcaddy compiler](https://github.com/caddyserver/xcaddy)

```shell
go install github.com/caddyserver/xcaddy/cmd/xcaddy@latest
```

Then just run the command below
```shell
xcaddy build --with github.com/crazy-goat/go-mesi/servers/caddy
```

Then we can check if caddy contains the right module using this command

```shell
caddy list-modules | grep mesi
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

Finally, you can start the Caddy server with the command:

```shell
caddy run --config Caddyfile
```