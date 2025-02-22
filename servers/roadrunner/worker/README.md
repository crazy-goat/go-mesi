# Example of mESI for RoadRunner
A very basic script that allows you to run and test mESI middleware for RoadRunner. To run it, execute the following commands

## Requirements
 - You must first build RoadRunner with mESI support. The easiest way to do this is to use a ready-made script found [here](../build.sh).
 - Additionally, you must have the PHP interpreter installed.
## Running

```shell
composer i
rr serve -d
```

Then, after starting the RoadRunner server, after entering the page http://127.0.0.1:8080/ you should see the message `Welcome to ESI Test`