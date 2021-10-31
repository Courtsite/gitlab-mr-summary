# gitlab-mr-summary

📰 A simple Google Cloud Function in Go to report a summary of GitLab merge requests to Microsoft Teams - it can be triggered regularly using Cloud Scheduler.

---

## Getting Started

### Prerequisites

- Ensure you have `gcloud` installed:
    - MacOS: `brew cask install google-cloud-sdk`
    - Others: https://cloud.google.com/sdk/gcloud
- Ensure you have authenticated with Google Cloud: `gcloud init`
- (Optional) Set your current working project: `gcloud config set project <project>`

### Deployment

1. Clone / download a copy of this repository
2. Copy `.env.sample.yaml` to `.env.yaml`, and modify the environment variables declared in the file
3. Run `./deploy.sh` _(recommendation: do not allow unauthenticated requests, see section on Cloud Scheduler below for more information)_

### Setting Up Cloud Scheduler

Optionally, if you would like to report a summary regularly (cron), modify, and run `./create_scheduler.sh` to create the necessary resources.
Unlike `./deploy.sh` which you can run multiple times, you should only run `./create_scheduler.sh` once.
This script will also set-up a service account which will be used to securely invoke the Cloud Function, without exposing it to the internet (specifically, preventing unauthenticated requests).

Alternatively, you can also set-up Cloud Scheduler manually via the [web UI](https://console.cloud.google.com/cloudscheduler) or through _infrastructure-as-code_, e.g. [Terraform](https://registry.terraform.io/providers/hashicorp/google/latest/docs).
