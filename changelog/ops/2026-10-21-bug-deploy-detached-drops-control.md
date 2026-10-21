A deploy started from an atrium card now keeps -Control and -IKnowItKillsMyShell when it detaches, and every other
switch too, so the hidden child no longer refuses what the card asked for. A detached child that refuses or fails before
its first step writes it to trium-deploy.log and says it to the -ReportTo card, so a deploy never ends in silence.
install-live-scripts.ps1 now copies oom-ssh.json and every other non-script file beside the scripts, which is why
the last deploy logged in to sgg as claude.