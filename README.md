# Infinity Metrics Installer

Infinity Metrics is a powerful, privacy-first, ai-powered web analytics platform designed for self-hosting. This repository contains zero-configuration installer for the Infinity Metrics analytics platform. This tool automates the entire setup process for running Infinity Metrics in production environments with Docker Swarm, zero-downtime updates, and automated backups.

## Features

- **One-command installation**: Get up and running in minutes with a single command
- **Zero-downtime updates**: Seamless upgrades without service interruption
- **Automated backups**: Built-in database backup scheduling
- **Secure by default**: Automatic HTTPS certificate management
- **Cross-platform**: Works on most Linux distributions
- **Self-contained**: No external dependencies beyond Docker

## Quick Start

Install Infinity Metrics with a single command:

```bash
curl -sSL https://getinfinitymetrics.com/install.sh | sudo bash
```


## Uninstalling

To completely remove Infinity Metrics from your system:

```bash
cd /opt/infinity-metrics
docker stack rm infinity-metrics
# Optional: remove the installation directory
sudo rm -rf /opt/infinity-metrics
```

## License

MIT License - See [LICENSE](LICENSE) for details.

## Support

- Documentation: [docs.infinitymetrics.com](https://docs.infinitymetrics.com)
- Issues: [GitHub Issues](https://github.com/karloscodes/infinity-metrics-installer/issues)

## Related Projects

- [Infinity Metrics Deployment](https://github.com/karloscodes/infinity-metrics-deployment): Docker Swarm configuration for Infinity Metrics
