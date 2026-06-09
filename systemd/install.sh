#! /bin/bash

sudo cp -v ./*.service /etc/systemd/system/
sudo systemctl daemon-reload
