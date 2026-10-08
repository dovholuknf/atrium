Tests and CI no longer start PowerShell off Windows. The daemon tests run sh on Linux and macOS, the PowerShell parse
check runs in the Windows job only, and the Linux CI image no longer installs pwsh. t-no-pwsh-on-linux.
