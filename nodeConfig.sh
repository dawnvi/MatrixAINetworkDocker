#!/bin/sh
#This is used to detect first time run and moves a file
if [ -f "/matrix/man.json" ]; then
  mv /matrix/man.json /matrix/chaindata/
fi

#This section creates your entrust.json file on the fly each time you start the container
#This requires that you mount a persistent volume from your host OS to the container's /matrix/chaindata directory
#You must have a signAccount.json file in your mounted chaindata directory
PASS="$(date +%s | sha256sum | base64 | head -c 4)pO1@"
echo $PASS > /matrix/gman.pass
echo $PASS >> /matrix/gman.pass
cat /matrix/gman.pass | /matrix/gman --datadir /matrix/chaindata aes --aesin /matrix/chaindata/signAccount.json --aesout /matrix/entrust.json

#This also detects if this is a first run and intializes the genisis block
if [ ! -d "/matrix/chaindata/gman" ]; then
  cd /matrix/ && ./gman --datadir /matrix/chaindata init /matrix/MANGenesis.json
fi

#This sets the wallet address based on the mounted persistent volume
MAN_WALLET="$(ls /matrix/chaindata/keystore/)"

#This starts the node using the port and wallet variables with maxpeers of 25 to reduce I/O overhead
cd /matrix/ && cat /matrix/gman.pass | ./gman --datadir /matrix/chaindata --networkid 1 --debug --verbosity 1 --port $MAN_PORT --manAddress $MAN_WALLET --entrust /matrix/entrust.json --gcmode archive --outputinfo 0 --syncmode full
