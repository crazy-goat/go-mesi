# ESI middleware for roadrunner
A lightweight implementation of Edge Side Includes (ESI) middleware for RoadRunner

## Building RoadRunner with mESI middleware
To add the mesi middleware to the RoadRunner server, you need to compile it properly. The best way to do this is to use the velox compiler

```shell
go install github.com/roadrunner-server/velox/v2024/cmd/vx@latest
```

Then you need to download the velox.toml file and add an entry for the mesi middleware to it
```toml
[github.plugins.mesi]
ref = "main"
owner = "crazy-goat"
repository = "go-mesi"
folder = "servers/roadrunner"
```

An alternative method is to use [this build script](build.sh):
```shell
./build.sh v2024.3.5
```
The script will download all dependencies and build RoadRunner with the mESI middleware.

## Configuration
To enable the mESI middleware, you must add the appropriate entry in the http module in the .rr.yaml configuration file.

```yaml
http:
  address: "0.0.0.0:8080"
  middleware:
    - "mesi"
```

An example script with the appropriate configuration can be found in the [worker](worker) directory