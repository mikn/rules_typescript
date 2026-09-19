import { onParse, type Props } from "./lib";

export const props: Props = { size: "sm", children: "x" };

export const seen: (string | number)[][] = [];

onParse((issue) => {
  seen.push(issue.path);
});
