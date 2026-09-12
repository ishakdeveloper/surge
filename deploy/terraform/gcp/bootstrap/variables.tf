variable "project_id" {
  description = "The project that holds the image registry. Images are built once and promoted, so staging and production pull from this one registry rather than each having their own."
  type        = string
}

variable "region" {
  description = "Eemshaven: the region closest to the one market this serves."
  type        = string
  default     = "europe-west4"
}

variable "github_repository" {
  description = "owner/name. Only workflows in this repository can exchange a GitHub token for a Google one."
  type        = string
  default     = "ishakdeveloper/surge"
}

variable "push_ref" {
  description = "The only ref whose workflows may push images. Branches and pull requests build and scan, and push nothing."
  type        = string
  default     = "refs/heads/main"
}
