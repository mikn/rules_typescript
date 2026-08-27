import type { GetServerSideProps } from "next";

type Props = { host: string };

export const getServerSideProps: GetServerSideProps<Props> = async (context) => {
  return { props: { host: context.req.headers.host ?? "unknown" } };
};

export default function Ssr({ host }: Props) {
  return (
    <main>
      <h1>getServerSideProps</h1>
      <p>Host: {host}</p>
    </main>
  );
}
