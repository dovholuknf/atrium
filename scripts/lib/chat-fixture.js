// The chat view's markup as the phone page draws it (m/js/card.js, answers.js, compose.js), for the colour check and the mock.
const SHEETS = ["css/tokens.css", "css/themes.css", "m/m.css", "m/compose.css", "m/changes.css", "m/cardurl.css", "m/working.css",
  "m/docs.css", "m/files.css", "m/changereq.css", "m/items.css", "m/push.css", "m/chat-embed.css"];
const ico = '<svg viewBox="0 0 24 24" width="22" height="22"><path d="M12 19V5M6 11l6-6 6 6" fill="none" stroke="currentColor" stroke-width="2.4"/></svg>';
const hdr = (who, acts) => '<header class="rh"><span class="src">' + who + '</span><time>4m ago</time>' +
  (acts ? '<span class="acts"><button type="button" class="bact">copy</button><button type="button" class="bact">reply</button></span>' : "") + "</header>";
const BODY = '<div class="app"><section class="sheet" id="m-card"><div class="sheet-scroll"><div id="m-replies" class="conv"><div class="replies">' +
  '<article class="reply">' + hdr("agent@room", true) + '<div class="md"><p>Here is <strong>the plan</strong> with a <a href="#">link</a> and <code>inline code</code>.</p>' +
  "<ul><li>first item</li><li>second item</li></ul><pre class=\"code\"><code>go test ./...</code></pre></div>" +
  '<button type="button" class="chg-chip">3 files edited</button></article>' +
  '<article class="reply mine">' + hdr("you", true) + '<div class="own">and my own words back</div></article>' +
  '<article class="reply mine pending">' + hdr("you", false) + '<div class="own">still sending</div></article>' +
  '</div></div><div id="m-card-extras"><div class="block questions"><h2>Open questions <span class="q-hint">tap one</span></h2><ol class="q-list">' +
  '<li class="q-item done"><button type="button" class="q-head"><span class="q-n">1</span><span class="q-main"><span class="q-text">Which branch?</span><span class="q-ans">main</span></span><span class="q-edit">edit</span></button></li>' +
  '<li class="q-item open"><button type="button" class="q-head"><span class="q-n">2</span><span class="q-main"><span class="q-text">Ship it now?</span></span></button>' +
  '<div class="q-form"><div class="q-chips"><button type="button" class="q-chip">Yes</button><button type="button" class="q-chip on">No</button></div>' +
  '<div class="q-row"><textarea class="q-box" rows="1" placeholder="Your answer"></textarea><button type="button" class="q-mic">' + ico + '</button><button type="button" class="q-done">Done</button></div></div></li>' +
  '</ol><button type="button" class="q-send">Send answers (1)</button><p class="quiet q-sendnote">1 of 2 answered. Only those are sent.</p>' +
  '<button type="button" class="q-send" disabled>Send answers</button></div></div></div>' +
  '<div id="m-compose"><div class="mc"><div class="mc-row with-attach"><button type="button" class="mc-attach">' + ico + '</button>' +
  '<textarea class="mc-box" rows="1" placeholder="Message this session"></textarea><button type="button" class="mc-mic">' + ico + '</button>' +
  '<button type="button" class="mc-send">' + ico + '</button></div></div></div></section></div>';
module.exports = { SHEETS, BODY };
