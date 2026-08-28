<script lang="ts">
  let { data } = $props();
  const heading: string = "acme-page-marker";
</script>

<h1>{heading}</h1>
<p>{data.hello}</p>

<style>
  h1 {
    color: rebeccapurple;
  }
</style>
