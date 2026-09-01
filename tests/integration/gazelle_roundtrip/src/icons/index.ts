import logo from "./logo.svg";

// Types against the directive's expression, not against asset_library's string
// default: `.viewBox` is TS2339 on a string.
export const box: string = logo.viewBox;
