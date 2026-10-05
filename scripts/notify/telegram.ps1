# An example notify command that sends a Telegram message. Atrium holds this
# script's name and never the bot token: the token is read here, from a file only
# you can read, and never goes in the command's argv. See scripts/notify/README.md.
#
# Set it with: PUT /_hub/notify {"enabled": true, "command": ["pwsh", "-NoProfile", "-File", "<path>/telegram.ps1"]}
#
# Reads the one JSON line atrium writes to stdin: name, reason, card, room.
# Sends a fixed line and the reason. The card name goes in only when
# ATRIUM_TELEGRAM_NAMES is set, because a name can carry a repo or a customer.
# Prints nothing but a fixed failure line, because the hub keeps the first stderr
# line and serves it back.
$in    = [Console]::In.ReadLine() | ConvertFrom-Json
$dir   = if ($env:ATRIUM_TELEGRAM_DIR) { $env:ATRIUM_TELEGRAM_DIR } else { "$HOME/.config/atrium-telegram" }
$token = (Get-Content "$dir/token" -Raw).Trim()
$chat  = (Get-Content "$dir/chat" -Raw).Trim()
$who   = if ($env:ATRIUM_TELEGRAM_NAMES) { $in.name } else { 'atrium: a card' }
$link  = 'https://<share>/m/#term=' + [uri]::EscapeDataString($in.card)
try {
    Invoke-RestMethod "https://api.telegram.org/bot$token/sendMessage" -Method Post -ErrorAction Stop `
        -Body @{ chat_id = $chat; text = "$who`: $($in.reason)`n$link"; disable_web_page_preview = 'true' } |
        Out-Null
} catch { [Console]::Error.WriteLine('telegram send failed'); exit 1 }
