Pasting a screenshot whose clipboard bytes had not rendered yet no longer saves a 0-byte file and pastes its path. The
board reads the clipboard again, and if the image is still empty it says "the pasted image was empty, paste it again".
The server refuses an empty upload outright. Item r-paste-empty-image.
