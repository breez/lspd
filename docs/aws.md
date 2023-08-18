## Automated deployment of LSPD stack to AWS
Cloudformation template for automated deployment of lspd, bitcoind and cln with postgresql backend.
### Requirements 
- AWS account 
- AWS SES configured

### Deployment 
[Cloudformation template](../deploy/deploy.yml) will automatically deploy several things:
- new ec2 instance (m6a.xlarge) to your selected VPC
- bitcoind
- clnd (with postgresql as backend)
- lspd

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