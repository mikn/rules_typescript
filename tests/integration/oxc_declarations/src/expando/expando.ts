// 7.0.2 crashes on this shape; the tsgo_source patch reports TS9013 instead.
declare function f(): { p: string };
export const g = () => f().p || "";
