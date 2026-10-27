terraform {
  required_providers {
    appsignal = {
      source = "sevenlabnl/appsignal"
    }
  }
}

# The token can also come from APPSIGNAL_TOKEN.
provider "appsignal" {}
