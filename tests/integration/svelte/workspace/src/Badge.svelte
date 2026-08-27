<script lang="ts">
  let { tone = "neutral" }: { tone?: string } = $props();
</script>

<span class="badge">acme-badge-marker {tone}</span>

<style>
  .badge {
    font-weight: 600;
  }
</style>
