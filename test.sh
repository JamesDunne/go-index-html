#!/bin/bash
sudo rm -f /tmp/index-html-band.test.sock
sudo GOMAXPROCS=2 -u www-data ./index-html -l unix:///tmp/index-html-band.test.sock -p /mp3 -xa /mp3-private -r /home/band/mp3 -jp-url /js -jp-path /srv/bittwiddlers.org/index-html/src/js -html /srv/bittwiddlers.org/index-html/src/html
