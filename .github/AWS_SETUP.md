# AWS Setup for Infinity Metrics Installer Workflow

This guide explains how to set up the necessary AWS IAM role and GitHub repository secrets to use the "Manual VM Setup with Infinity Metrics" workflow.

## Creating an AWS IAM Role for GitHub Actions

1. **Create an OpenID Connect (OIDC) identity provider in AWS**:
   - Go to the AWS IAM console
   - Navigate to "Identity providers" and click "Add provider"
   - Select "OpenID Connect"
   - For the provider URL, enter: `https://token.actions.githubusercontent.com`
   - For the Audience, enter: `sts.amazonaws.com`
   - Click "Add provider"

2. **Create an IAM role**:
   - In the IAM console, go to "Roles" and click "Create role"
   - Select "Web identity" as the trusted entity type
   - For the Identity provider, select the GitHub OIDC provider you just created
   - For the Audience, select `sts.amazonaws.com`
   - Add a condition to restrict which repositories can use this role:
     ```
     {
       "StringLike": {
         "token.actions.githubusercontent.com:sub": "repo:YOUR-GITHUB-USERNAME/infinity-metrics-installer:*"
       }
     }
     ```
     (Replace `YOUR-GITHUB-USERNAME` with your actual GitHub username)
   - Click "Next"

3. **Attach permissions policies**:
   - Attach the following AWS managed policies:
     - `AmazonEC2FullAccess` (or create a more restrictive custom policy)
   - Click "Next"

4. **Name and create the role**:
   - Enter a role name (e.g., `GitHubActionsInfinityMetricsRole`)
   - Add a description (e.g., "Role for GitHub Actions to deploy Infinity Metrics")
   - Click "Create role"

5. **Copy the Role ARN**:
   - After creating the role, click on it to view its details
   - Copy the "Role ARN" (it should look like `arn:aws:iam::123456789012:role/GitHubActionsInfinityMetricsRole`)

## Adding the AWS Role ARN to GitHub Secrets

1. **Go to your GitHub repository**:
   - Navigate to your forked or cloned infinity-metrics-installer repository

2. **Add the repository secret**:
   - Go to "Settings" > "Secrets and variables" > "Actions"
   - Click "New repository secret"
   - Name: `AWS_ROLE_ARN`
   - Value: Paste the Role ARN you copied from AWS
   - Click "Add secret"

## Using the Workflow

Once you've set up the AWS role and GitHub secret, you can use the "Manual VM Setup with Infinity Metrics" workflow:

1. Go to the "Actions" tab in your GitHub repository
2. Select the "Manual VM Setup with Infinity Metrics" workflow
3. Click "Run workflow"
4. Fill in the required information
5. Click "Run workflow" to start the deployment

The workflow will use the AWS role to create resources in your AWS account without requiring you to store AWS access keys in GitHub.
