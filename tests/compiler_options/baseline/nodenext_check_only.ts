// declarations = "oxc" needs the annotation; the point of the target is that the
// NodeNext module/resolver pair reaches tsgo coherently on this path too.
export function scheme(secure: boolean): string {
  return secure ? "https" : "http";
}
