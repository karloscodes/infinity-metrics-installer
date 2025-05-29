# Infinity Metrics Installer

Infinity Metrics is a powerful, privacy-first, ai-powered web analytics platform designed for self-hosting. This repository contains zero-configuration installer for the Infinity Metrics analytics platform. 

## Features

- **One-command installation**: Get up and running in minutes with a single command
- **Zero-downtime updates**: Seamless upgrades without service interruption
- **Automated backups**: Built-in database backup scheduling
- **Secure by default**: Automatic HTTPS certificate management
- **GitHub Actions workflow**: Easily deploy to AWS with a manual workflow

## Installation Options

### Local Installation

Run the installer directly on your server:

```bash
./infinity-metrics install
```

### AWS VM Setup via GitHub Actions

You can use our GitHub Actions workflow to automatically set up a new VM in AWS with Infinity Metrics installed:

1. Go to the "Actions" tab in your GitHub repository
2. Select the "Manual VM Setup with Infinity Metrics" workflow
3. Click "Run workflow"
4. Fill in the required information:
   - AWS Region (e.g., us-east-1)
   - EC2 Instance Type (e.g., t2.micro)
   - Domain for Infinity Metrics (e.g., analytics.example.com)
   - Admin Email Address
   - Infinity Metrics License Key
5. Click "Run workflow" to start the deployment

The workflow will:
- Create a new EC2 instance in your AWS account
- Install Docker and required dependencies
- Set up Infinity Metrics with your configuration
- Provide you with access details when complete

**Prerequisites:**
- An AWS account with appropriate permissions
- A domain with DNS records that can be pointed to the new server
- A valid Infinity Metrics license key

## License

MIT License - See [LICENSE](LICENSE) for details.

## Support

- Documentation: [getinfinitymetrics.com](https://getinfinitymetrics.com)
- Issues: [GitHub Issues](https://github.com/karloscodes/infinity-metrics-installer/issues)
