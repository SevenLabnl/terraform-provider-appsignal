terraform {
  required_providers {
    appsignal = {
      source = "sevenlabnl/appsignal"
    }
  }
}

# The token can also come from APPSIGNAL_API_TOKEN.
provider "appsignal" {}
