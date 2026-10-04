import { createRoot } from 'react-dom/client';
import { EditorContent, useEditor } from '@tiptap/react';
import StarterKit from '@tiptap/starter-kit';

function BaselineProbe() {
  const editor = useEditor({
    extensions: [StarterKit],
    content: '<p>Trove Phase 0 build probe</p>',
  });
  return <main><h1>Trove baseline</h1><EditorContent editor={editor} /></main>;
}

createRoot(document.getElementById('root')!).render(<BaselineProbe />);
