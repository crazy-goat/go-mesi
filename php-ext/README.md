# PHP mESI Extension
This repository contains a PHP extension that wraps the mESI library, a lightweight Edge Side Includes (ESI) implementation written in Go. By integrating mESI’s functionalities, this extension brings minimal but correct ESI processing to your PHP-based environment.

## Requirements

To build this PHP extension, you need Golang and the necessary dependencies for compiling PHP extensions. Install them using the following command on Debian-based systems:
```
sudo apt-get update && sudo apt-get install -y \
    golang \
    php-dev \
    build-essential \
    autoconf \
    bison \
    re2c \
    libxml2-dev
```

## Installation

Clone this repository:
```
git clone https://github.com/crazy-goat/go-mesi.git
cd go-mesi
```

Before building the PHP extension, you must first compile and install the libgomesi library. To do this, execute the following commands:
```
cd libgomesi
make
sudo make install
```
This step ensures that the required Go-based library is available for the PHP extension to link against.

Now you can proceed with building the PHP mESI extension. Follow these steps:
```
cd php-ext
phpize
./configure
make
sudo make install
```
This will compile, test, and install the PHP extension, making it ready for use in your environment.

# Enabling extension

To enable the **mESI PHP extension** on **Debian** or **Ubuntu**, follow these steps:

```
echo "extension=mesi.so" | sudo tee /etc/php/$(php -r 'echo PHP_MAJOR_VERSION.".".PHP_MINOR_VERSION;')/mods-available/mesi.ini
```

Activate the extension using `phpenmod`:
```
sudo phpenmod mesi
```

If you are using PHP-FPM, restart the service:
```
sudo systemctl restart php-fpm
```

Check if the extension is loaded correctly:
```
php -m | grep mesi
```

Hello world! example script:
```php
<php 
echo \mesi\parse('<!--esi Hello, world!-->', 5, "http://127.0.0.1");
```