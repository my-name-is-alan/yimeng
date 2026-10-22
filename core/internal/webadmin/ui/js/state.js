// Shared app state (current device, admin flag, navigation).
export const state = { me: null, system: null };
export const isAdmin = () => !!state.me?.admin;
export const nav = (hash) => { if (location.hash === hash) dispatchEvent(new HashChangeEvent("hashchange")); else location.hash = hash; };
