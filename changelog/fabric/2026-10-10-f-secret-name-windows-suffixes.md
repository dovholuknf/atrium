# Secret file names are matched the way Windows reads them

`key.pem.`, `key.pem `, `a.pem::$DATA` and `.env:alt` now count as `key.pem`, `a.pem` and `.env` when a document path is
checked for a secret name, on every platform. They used to slip past the check. `f-secret-name-windows-suffixes`.
