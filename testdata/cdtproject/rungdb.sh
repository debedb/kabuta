#!/bin/sh
gcc -g main.c -lpthread
sudo expect -f ./rungdb.expect