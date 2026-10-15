package workspace

// These mode-specific instructions guide generation without interpreting a
// request or source to synthesize a draft. Existing output contracts still apply.
const aiExpandInstructions = "Expand wording and structure only, preserving every recorded fact and limitation. Do not turn plausible benefits, motives, standards or future plans into facts. Attribute claims or proposals only when stated in the source; label any requested new suggestions as conditional."

const aiMermaidDraftInstructions = `Return only Mermaid source, with no Markdown fence. Use flowchart, sequenceDiagram, stateDiagram-v2, classDiagram, erDiagram, gantt, pie, mindmap or timeline. Put the diagram declaration on its own line and each statement on a separate line. Use the diagram's actual relationship syntax; flowchart edges use arrows such as A --> B, not combined node names. Preserve requested labels, direction and relationships. Do not use links, click actions, HTML or init directives.`

const aiCommentDraftInstructions = ` Add useful comments while preserving all code, logic and behavior. Use the source language's comment syntax. End a line comment with an actual source newline before subsequent code (escaped as \n in JSON); a space does not end it. Use a block comment only if the language supports it.`
