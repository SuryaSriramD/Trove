package workspace

// These mode-specific instructions guide generation without interpreting a
// request or source to synthesize a draft. Existing output contracts still apply.

const aiCommentDraftInstructions = ` Add useful comments while preserving all code, logic and behavior. Use the source language's comment syntax. End a line comment with an actual source newline before subsequent code (escaped as \n in JSON); a space does not end it. Use a block comment only if the language supports it.`
