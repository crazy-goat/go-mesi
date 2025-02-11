# Nginx ESI module

This repository contains a Nginx module that wraps the mESI library, a lightweight Edge Side Includes (ESI) implementation written in Go. 
By integrating mESI’s functionalities, this module brings minimal but correct ESI processing to Nginx server.

## Requirements

To build this PHP extension, you need Golang and the necessary dependencies for compiling PHP extensions. Install them using the following command on Debian-based systems:
```
sudo apt-get update && sudo apt-get install -y \
    golang \
    build-essential \
    autoconf \
    bison \
    re2c \
    libxml2-dev \
    zlib1g-dev
```

## Installation

Clone this repository:
```
git clone https://github.com/crazy-goat/go-mesi.git
cd go-mesi
```

Before building the Nginx module, you must first compile and install the libgomesi library. To do this, execute the following commands:
```
cd libgomesi
make
sudo make install
```
This step ensures that the required Go-based library is available for the Nginx module to link against.

Now you can proceed with building the PHP mESI extension. Follow these steps:
```
cd servers/nginx
./build.sh
```

If the build goes well, you will find the nginx module file in this path: 
```
build/nginx/modules/ngx_http_mesi_module.so
```

# Enabling module

To enable the mESI module, add the following line to the main Nginx configuration file (e.g., nginx.conf):

```nginx configuration
load_module modules/ngx_http_mesi_module.so;
```

To enable the mESI module for a specific location in the HTTP server, add the following option:
```nginx configuration
enable_mesi on;
```
to the location section of the server configuration. For example:
```nginx configuration
location / {
    enable_mesi on;
    root   ../../tests;
    index  index.html;
}
```

[Here](nginx.conf) you can find full example configuration
