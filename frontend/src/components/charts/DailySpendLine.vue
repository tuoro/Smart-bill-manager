<template>
  <VChart class="chart" :option="option" autoresize />
</template>

<script setup lang="ts">
import { use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { LineChart } from 'echarts/charts'
import { GridComponent, TooltipComponent, LegendComponent } from 'echarts/components'
import VChart from 'vue-echarts'

use([CanvasRenderer, LineChart, GridComponent, TooltipComponent, LegendComponent])

defineProps<{
  option: Record<string, unknown>
}>()
</script>

<style scoped>
.chart {
  width: 100%;
  height: 260px;
}
</style>

