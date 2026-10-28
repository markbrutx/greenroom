# Greenroom on AWS

One EC2 instance (t4g.micro by default) with Docker, Caddy for HTTPS and an
Elastic IP. No SSH: manage it through SSM. About $11 a month in eu-central-1.

## Create

```sh
aws ssm put-parameter --name /greenroom/password --type SecureString --value 'team password'
AMI=$(aws ssm get-parameter --name /aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-arm64 --query Parameter.Value --output text)
aws cloudformation create-stack --stack-name greenroom-demo \
  --template-body file://deploy/aws/greenroom.yaml --capabilities CAPABILITY_IAM \
  --parameters ParameterKey=Domain,ParameterValue=example.com \
               ParameterKey=RedirectFrom,ParameterValue=www.example.com \
               ParameterKey=ImageTag,ParameterValue=0.2.0 \
               ParameterKey=ImageId,ParameterValue=$AMI
aws cloudformation describe-stacks --stack-name greenroom-demo --query 'Stacks[0].Outputs'
```

Point an A record for the domain at `PublicIp` and a CNAME for
`RedirectFrom` at the domain. Caddy gets the certificates once DNS resolves.
`RedirectFrom` is optional.

## Upgrade

```sh
aws ssm send-command --instance-ids INSTANCE_ID --document-name AWS-RunShellScript \
  --parameters 'commands=["cd /opt/greenroom && sed -i s/^TAG=.*/TAG=0.3.0/ .env && docker compose pull && docker compose up -d"]'
```

## Shell

```sh
aws ssm start-session --target INSTANCE_ID
```

## Delete

```sh
aws cloudformation delete-stack --stack-name greenroom-demo
aws ssm delete-parameter --name /greenroom/password
```
