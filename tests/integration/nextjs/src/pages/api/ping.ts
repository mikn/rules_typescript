import type { NextApiRequest, NextApiResponse } from "next";

export default function handler(
  _request: NextApiRequest,
  response: NextApiResponse<{ pong: boolean }>,
): void {
  response.status(200).json({ pong: true });
}
