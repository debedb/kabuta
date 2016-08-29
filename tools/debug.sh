#!/bin/sh
killall dlv 
killall kabuta
rm ~/kabuta.log 
go install github.com/debedb/kabuta/kabuta
echo > ~/kabuta.log
tail -f ~/kabuta.log 
