# qbconf

![Logo](https://img.raftech.nl/logo-qbconf.png)

A minimalistic CLI to generate kubeconfig 

#
[![License](https://img.shields.io/github/license/raftechnl/terrafile)](./LICENSE)


## Functionality

Minimalistic Kubernetes kubeconfig file generator using AWS STS and EKS APIs. It supports role assumption and Github Actions OIDC out of the box! 

Its small footprint of 4MBs and single responsibility makes it ideal for use in CI/CD pipelines.

## Installing

### Download
> Check our release page to download a specific version

```shell
    #!/bin/bash

    # Fetch the latest release version from Github API
    VERSION=$(curl --silent "https://api.github.com/repos/raftechio/qbconf/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

    # Set the URL of the tarball for the latest release
    URL="https://github.com/raftechio/qbconf/releases/download/${VERSION}/qbconf_${VERSION}_darwin_x86_64.tar.gz"

    # Download and install the latest release
    curl -L ${URL} | tar xz
    chmod +x qbconf
    sudo mv qbconf /usr/local/bin/
```

### Homebrew
```shell
brew tap  RaftechNL/toolbox
brew install raftechnl/toolbox/qbconf
```

## Usage
CLI supports the following actions
* generate `<cloud>` - generates a kubeconfig file for a cluster in selected cloud provider

### generate
Generate is our root working command. It supports cloud providers ( AWS at the moment ).

#### AWS
Authentication is selected with a single `--auth` flag: `default` (SDK credential chain), `assume-role`, `gha-oidc` (GitHub Actions), or `gitlab-oidc` (GitLab CI/CD).

```
## generates kubeconfig for aws eks cluster using the default AWS credential chain
qbconf generate aws --cluster-name XXX --region us-east-1

## generates kubeconfig for aws eks cluster by assuming the given role ( uses provided credentials )
qbconf generate aws --cluster-name XXX --region us-east-1 --auth assume-role --role-arn "arn:aws:iam::12334556:role/AWSMagicRole"

## generates kubeconfig for aws eks cluster by assuming the given role via GitHub Actions OIDC
qbconf generate aws --cluster-name XXX --region us-east-1 --auth gha-oidc --role-arn "arn:aws:iam::12334556:role/AWSMagicRole"

## generates kubeconfig for aws eks cluster by assuming the given role via GitLab CI/CD OIDC
qbconf generate aws --cluster-name XXX --region us-east-1 --auth gitlab-oidc --role-arn "arn:aws:iam::12334556:role/AWSMagicRole"
```

> The v1 flags `--with-assume-role`, `--with-gha-oidc` and `--with-gitlab-oidc` still work but are deprecated aliases for `--auth`.

##### Configuration
Every flag can also be provided via environment variables (`QBCONF_CLUSTER_NAME`, `QBCONF_REGION`, ...; `AWS_REGION`, `AWS_ROLE_ARN` and `AWS_ROLE_SESSION_NAME` remain honored) or a config file (`./.qbconf.yaml`, `$HOME/.config/qbconf/.qbconf.yaml`, or `--config <path>`):

```yaml
# .qbconf.yaml
cluster-name: my-cluster
region: us-east-1
auth: assume-role
role-arn: arn:aws:iam::12334556:role/AWSMagicRole
```

Precedence: flags > environment > config file > defaults.

##### Output
The CLI will by default output a kubeconfig file called `kubeconfig.yaml` (written with `0600` permissions). This can be changed by using the `--output-file` flag.

##### Logging
Logs go to stderr and are quiet by default. Use `--log-level debug` for verbose output and `--log-format json|console|auto` to control the format (`auto` picks console on a terminal, JSON in CI).

## Contributing

Contributions are always welcome!


## Authors

- [@rafpe](https://www.github.com/rafpe)
