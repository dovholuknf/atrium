Back from a document in the board's documents panel now always returns to the document list, and back again closes the
panel. A board that was still starting up could overwrite the list's history entry, so back skipped the list and closed
the panel, which made the same click do two different things (u-board-docs-back-keeps-panel).
