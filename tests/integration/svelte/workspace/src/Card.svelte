<script lang="ts">
  interface Props {
    label: string;
    count?: number;
  }

  let { label, count = 0 }: Props = $props();
  const shout = (text: string): string => text.toUpperCase();
</script>

<p class="card">{shout(label)} acme-card-marker {count}</p>

<style>
  .card {
    color: rebeccapurple;
  }
</style>
