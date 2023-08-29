## Automated install of LSPD stack for linux
### Requirements
- ubuntu or debian based distribution 
- AWS SES credentials
- root / user without sudo password 

### Installation 
To install bitcoind,cln and lspd to your system simply run:

```curl -sL  https://raw.githubusercontent.com/breez/lspd/master/deploy/lspd-install.sh  | sudo bash -```

It will automatically configure your server and install all needed dependencies for running LSPD stack. You will have to manually change the name of your LSPD and your cln alias.

LSPD:

```
vim /home/lspd/.env
# change the name variable in the last line, it will have randomly generated name like "lsp-53v4"
NODES='[ { "name": "${LSPName}"
```

CLN:
```
vim /home/lightning/.lightning/config
# change alias row, it will have randomly generated name like "lsp-53v4"
alias="${LSPName}"
```

### After deployment steps
#### Configure email notifications

Edit file ```/home/lspd/.env```.

1) set your SES credentials:
```
AWS_REGION="<REPLACE ME>"
AWS_ACCESS_KEY_ID="<REPLACE ME>"
AWS_SECRET_ACCESS_KEY="<REPLACE ME>"
```

2) configure email
```
OPENCHANNEL_NOTIFICATION_TO='["REPLACE ME <email@example.com>"]'
OPENCHANNEL_NOTIFICATION_CC='["REPLACE ME <test@example.com>"]'
OPENCHANNEL_NOTIFICATION_FROM="test@example.com"

CHANNELMISMATCH_NOTIFICATION_TO='["REPLACE ME <email@example.com>"]'
CHANNELMISMATCH_NOTIFICATION_CC='["REPLACE ME <email@example.com>"]'
CHANNELMISMATCH_NOTIFICATION_FROM="replaceme@example.com"
```
#### Backup credentials

All credentials are generated automatically and are written down in ```/home/lspd/credentials.txt```

**Store them securely and delete the file.**
### Debugging 
Log file of deployment is written to ```/tmp/deployment.log``` where you can see the entire output of what happend during deployment. 