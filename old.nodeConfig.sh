#!/bin/sh
#This is used to detect first time run and moves a file
if [ -f "/go-matrix/man.json" ]; then
  mv /go-matrix/man.json /go-matrix/chaindata/
fi

#This section creates your entrust.json file on the fly each time you start the container
#This requires that you mount a persistent volume from your host OS to the container's /go-matrix/chaindata directory
#You must have a signAccount.json file in your mounted chaindata directory
PASS="$(date +%s | sha256sum | base64 | head -c 4)pO1@"
echo $PASS > /go-matrix/gman.pass
echo $PASS >> /go-matrix/gman.pass
cat /go-matrix/gman.pass | /go-matrix/build/bin/gman --datadir /go-matrix/chaindata aes --aesin /go-matrix/chaindata/signAccount.json --aesout /go-matrix/entrust.json

#This also detects if this is a first run and intializes the genisis block
if [ ! -d "/go-matrix/chaindata/gman" ]; then
  cd /go-matrix/build/bin && ./gman --datadir /go-matrix/chaindata init /go-matrix/MANGenesis.json
fi

#This sets the wallet address based on the mounted persistent volume
MAN_WALLET="$(ls /go-matrix/chaindata/keystore/)"

#This starts the node using the port and wallet variables
cd /go-matrix/build/bin && cat /go-matrix/gman.pass | ./gman --datadir /go-matrix/chaindata --networkid 1 --debug --verbosity 1 --port $MAN_PORT --manAddress $MAN_WALLET --entrust /go-matrix/entrust.json --gcmode archive --outputinfo 1 --syncmode full
