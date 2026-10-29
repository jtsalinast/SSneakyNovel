import { writable } from 'svelte/store';

// Writing registers the shell toolbar's real actions here; the shell renders
// the mockup toolbar and calls these. Null means "not available right now".
export const writingToolbar = writable({ save: null, addScene: null, accept: null });
