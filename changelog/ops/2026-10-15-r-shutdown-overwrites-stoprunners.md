A second `POST /v1/shutdown` that asks for a different `runners` value than the stop already in flight is now refused
with 409 naming the value in force, instead of overwriting it. Requests that agree still join. r-shutdown-overwrites-stoprunners.
