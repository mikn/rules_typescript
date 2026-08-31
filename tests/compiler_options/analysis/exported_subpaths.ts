import { version } from "pulse";
import { button } from "pulse/button";
import { accent } from "pulse/tokens/color";

export const label: string = button(version) + accent;
