# Chat: an image path in a message shows a thumbnail

A message that names an image (png, jpg, gif, webp) inside the card's folder shows it under the text, at most 240px
wide. The operator's own messages get it too, and the path stays in the text as a link. A relative path such as
`.atrium/incoming/x.png` works as well as an absolute one. A path outside the card stays plain text and is never asked
for. Item u-chat-image-thumbnails.
