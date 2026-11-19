<template>
  <!-- Custom Home Content: Full Page Mode -->
  <div v-if="hasHomeContent" class="min-h-screen">
    <!-- iframe mode -->
    <iframe
      v-if="isHomeContentUrl"
      :src="homeContent.trim()"
      class="h-screen w-full border-0"
      :sandbox="embeddedSandbox"
      :title="siteName"
      referrerpolicy="no-referrer"
      allowfullscreen
    ></iframe>
    <div v-else v-html="sanitizedHomeContent"></div>
  </div>

  <!-- Compact Home Page -->
  <div
    v-else-if="compactHomeEnabled"
    data-testid="compact-home"
    class="compact-portal flex min-h-screen flex-col bg-[var(--background)] text-[var(--foreground)]"
  >
    <header class="border-b border-[var(--border)] px-4 py-4 sm:px-6 ">
      <nav class="mx-auto flex max-w-5xl flex-wrap items-center justify-between gap-3 sm:gap-4">
        <div class="flex min-w-0 flex-1 items-center gap-3">
          <img
            v-if="siteLogo"
            :src="siteLogo"
            alt="Logo"
            class="h-9 w-9 shrink-0 rounded-lg object-contain"
          />
          <svg v-else class="h-9 w-9 shrink-0" viewBox="0 0 32 32" fill="none" role="img" aria-label="OmniHive"><path d="M16 2 28 9v14L16 30 4 23V9Z" fill="var(--primary)" /><path d="M16 6 24.5 11v10L16 26 7.5 21V11Z" stroke="var(--brand)" stroke-width="1.5" /><path d="m16 11 4.3 2.5v5L16 21l-4.3-2.5v-5Z" fill="var(--brand)" /></svg>
          <span class="min-w-0 truncate text-base font-semibold">{{ siteName }}</span>
        </div>
        <div class="flex max-w-full shrink-0 flex-wrap items-center justify-end gap-2">
          <LocaleSwitcher />
          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg text-[var(--muted-foreground)] hover:bg-[var(--surface-2)]  "
            :title="t('home.viewDocs')"
            :aria-label="t('home.viewDocs')"
          >
            <Icon name="book" size="md" />
          </a>
          <router-link
            v-if="showModelPlazaEntry"
            to="/model-plaza"
            class="flex h-10 shrink-0 items-center gap-1.5 rounded-lg px-2.5 text-sm font-medium text-[var(--muted-foreground)] hover:bg-[var(--surface-2)] hover:text-[var(--foreground)]   "
            :title="t('nav.modelPlaza')"
            :aria-label="t('nav.modelPlaza')"
          >
            <Icon name="grid" size="md" />
            <span class="hidden sm:inline">{{ t('nav.modelPlaza') }}</span>
          </router-link>
          <button
            class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg text-[var(--muted-foreground)] hover:bg-[var(--surface-2)]  "
            :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
            :aria-label="isDark ? t('home.switchToLight') : t('home.switchToDark')"
            @click="toggleTheme"
          >
            <Icon v-if="isDark" name="sun" size="md" />
            <Icon v-else name="moon" size="md" />
          </button>
          <router-link
            :to="isAuthenticated ? dashboardPath : '/login'"
            class="inline-flex min-h-10 shrink-0 items-center justify-center rounded-lg bg-[var(--primary)] px-4 py-2 text-sm font-medium text-white hover:bg-[var(--primary)]   "
          >
            {{ isAuthenticated ? t('home.dashboard') : t('home.login') }}
          </router-link>
        </div>
      </nav>
    </header>

    <main class="flex min-w-0 flex-1 items-center justify-center px-4 py-16 sm:px-6">
      <div class="min-w-0 max-w-2xl text-center">
        <img
          v-if="siteLogo"
          :src="siteLogo"
          alt="Logo"
          class="mx-auto mb-6 h-20 w-20 rounded-2xl object-contain"
        />
        <div v-else class="compact-emblem mx-auto mb-6 flex h-24 w-24 items-center justify-center rounded-2xl border border-[var(--border)] bg-[var(--card)]"><svg class="h-16 w-16" viewBox="0 0 32 32" fill="none" role="img" aria-label="OmniHive"><path d="M16 2 28 9v14L16 30 4 23V9Z" fill="var(--primary)" /><path d="M16 6 24.5 11v10L16 26 7.5 21V11Z" stroke="var(--brand)" stroke-width="1.5" /><path d="m16 11 4.3 2.5v5L16 21l-4.3-2.5v-5Z" fill="var(--brand)" /></svg></div>
        <h1 class="[overflow-wrap:anywhere] text-3xl font-bold md:text-4xl">{{ siteName }}</h1>
        <p class="mt-4 whitespace-pre-wrap [overflow-wrap:anywhere] text-base text-[var(--muted-foreground)] ">{{ siteSubtitle }}</p>
        <router-link
          :to="isAuthenticated ? dashboardPath : '/login'"
          class="mt-8 inline-flex min-h-10 items-center justify-center rounded-lg bg-primary-600 px-5 py-2.5 text-sm font-medium text-white hover:bg-primary-700"
        >
          {{ isAuthenticated ? t('home.goToDashboard') : t('home.login') }}
        </router-link>
      </div>
    </main>

    <footer class="min-w-0 border-t border-[var(--border)] px-4 py-5 text-center text-sm text-[var(--muted-foreground)] [overflow-wrap:anywhere] sm:px-6  ">
      &copy; {{ currentYear }} {{ siteName }}
    </footer>
  </div>

  <!-- Default Home Page: OmniHive High-Fidelity Portal -->
  <div
    v-else
    :class="['omnihive-portal relative flex min-h-screen flex-col', { 'bee-pause': beePaused }]"
  >
    <!-- Full-Width Sticky Glass Public Topbar -->
    <header id="pubNav" ref="publicNav" class="omnihive-glass-bar sticky top-0 z-40 border-b" @keydown.esc="closeNavMenu(true)">
      <nav class="mx-auto flex min-h-14 max-w-[1600px] flex-wrap items-center justify-between gap-3 px-4 py-2 sm:px-6">
        <!-- Left: Brand Honeycomb Emblem + Site Name + CmdK Trigger -->
        <div class="flex min-w-0 items-center gap-3">
          <span class="lg:hidden"><button id="public-menu-toggle" type="button" class="omnihive-icon-btn" aria-label="公开导航" :aria-expanded="mobileMenuOpen" aria-controls="public-mobile-menu" @click="mobileMenuOpen = !mobileMenuOpen">☰</button></span>
          <a href="#" class="group flex min-w-0 items-center gap-2.5 text-left">
            <div class="relative flex h-9 w-9 shrink-0 items-center justify-center rounded-xl border border-amber-500/25 bg-[var(--card)] shadow-sm transition-transform duration-200 group-hover:scale-105 ">
              <svg width="24" height="24" viewBox="0 0 32 32" fill="none" aria-hidden="true">
                <path d="M16 2 28.1 9v14L16 30 3.9 23V9Z" fill="var(--primary, #16161a)" />
                <path d="M16 5.6 25 10.8v10.4L16 26.4 7 21.2V10.8Z" stroke="var(--brand, #f59e0b)" stroke-width="1.8" />
                <path d="M16 10.2 21 13.1v5.8L16 21.8l-5-2.9v-5.8Z" fill="var(--brand, #f59e0b)" />
                <circle cx="16" cy="16" r="2.1" fill="var(--primary, #16161a)" />
              </svg>
            </div>
            <div class="min-w-0 leading-tight">
              <div class="flex items-center gap-1.5">
                <span class="truncate text-sm font-bold tracking-tight">OmniHive</span>
                <span class="omnihive-hive-pill hidden sm:inline-block">HIVE</span>
              </div>
              <div class="hidden truncate text-[10px] opacity-60 sm:block">智能算力中转蜂巢</div>
            </div>
          </a>

          <!-- ⌘K Command Palette Trigger -->
          <button
            type="button"
            class="omnihive-cmdk-btn hidden md:flex"
            aria-label="搜索模型和页面 (⌘K)"
            @click="triggerCommandPalette"
          >
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <circle cx="11" cy="11" r="8" />
              <path d="m21 21-4.3-4.3" />
            </svg>
            <span class="w-32 text-left text-xs">搜索模型 / 页面…</span>
            <span class="omnihive-kbd">⌘K</span>
          </button>
        </div>

        <!-- Center: Section Anchor Links -->
        <div class="public-menus hidden items-center gap-1 text-xs font-medium lg:flex">
          <div v-for="menu in NAV_MENUS" :key="menu.title" class="relative">
            <button
              :id="`nav-${menu.key}`"
              class="omnihive-nav-link rounded-md px-2 py-2"
              :aria-expanded="activeNavMenu === menu.key"
              :aria-controls="`menu-${menu.key}`"
              @click="activeNavMenu = activeNavMenu === menu.key ? '' : menu.key"
            >{{ menu.title }} <span aria-hidden="true">⌄</span></button>
            <div v-if="activeNavMenu === menu.key" :id="`menu-${menu.key}`" class="omnihive-dropdown left-0 mt-2 w-52 p-1.5" :aria-label="`${menu.title}菜单`">
              <button v-for="item in menu.items" :key="item[0]" class="omnihive-dropdown-item text-left text-sm" @click="navAction(item[1], item[0])">
                {{ item[0] }}
              </button>
            </div>
          </div>
        </div>

        <!-- Right: Actions (Model Plaza, Key Usage, Docs, 5 Theme Directions, Locale, Dark Mode, Login/Console) -->
        <div class="flex shrink-0 items-center gap-1.5 sm:gap-2">
          <router-link :to="isAuthenticated ? dashboardPath : '/login'" class="omnihive-nav-link hidden text-xs xl:inline">控制台 ›</router-link>
          <!-- Model Plaza Link (strictly guarded by showModelPlazaEntry) -->
          <router-link
            v-if="showModelPlazaEntry"
            to="/model-plaza"
            class="omnihive-action-pill"
            :title="t('nav.modelPlaza')"
            :aria-label="t('nav.modelPlaza')"
          >
            <Icon name="grid" size="sm" />
            <span class="hidden sm:inline">{{ t('nav.modelPlaza') }}</span>
          </router-link>

          <!-- Doc Link (strictly preserves test contract) -->
          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="omnihive-icon-btn"
            :title="t('home.viewDocs')"
            :aria-label="t('home.viewDocs')"
          >
            <Icon name="book" size="md" />
          </a>

          <!-- Language Switcher -->
          <LocaleSwitcher />

          <!-- Theme Toggle (strictly preserves test contract title === aria-label) -->
          <button
            type="button"
            class="omnihive-icon-btn"
            :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
            :aria-label="isDark ? t('home.switchToLight') : t('home.switchToDark')"
            @click="toggleTheme"
          >
            <Icon v-if="isDark" name="sun" size="md" />
            <Icon v-else name="moon" size="md" />
          </button>

          <!-- Login / Dashboard Primary CTA -->
          <router-link
            :to="isAuthenticated ? dashboardPath : '/login'"
            class="omnihive-btn-primary px-3.5 py-1.5 text-xs font-semibold"
          >
            <span
              v-if="isAuthenticated && userInitial"
              class="flex h-4 w-4 items-center justify-center rounded-full bg-amber-500 text-[10px] font-bold text-white"
            >
              {{ userInitial }}
            </span>
            <span>{{ isAuthenticated ? t('home.dashboard') : t('home.login') }}</span>
            <span aria-hidden="true">→</span>
          </router-link>
          <router-link v-if="!isAuthenticated" to="/register" class="omnihive-btn-outline px-3 py-1.5 text-xs font-semibold">免费注册</router-link>
        </div>
      </nav>
      <nav v-if="mobileMenuOpen" id="public-mobile-menu" class="grid grid-cols-2 gap-1 border-t border-[var(--border)] bg-[var(--card)] p-4 text-sm lg:hidden" aria-label="公开页面">
        <router-link v-if="showModelPlazaEntry" to="/model-plaza" class="rounded p-3" @click="mobileMenuOpen = false">模型广场</router-link>
        <router-link to="/key-usage" class="rounded p-3" @click="mobileMenuOpen = false">查用量</router-link>
        <a href="#rankings" class="rounded p-3" @click="mobileMenuOpen = false">排行榜</a>
        <router-link to="/help" class="rounded p-3" @click="mobileMenuOpen = false">接入指南</router-link>
        <a href="#architecture" class="rounded p-3" @click="mobileMenuOpen = false">架构对比</a>
        <a href="#faq" class="rounded p-3" @click="mobileMenuOpen = false">FAQ</a>
        <router-link :to="isAuthenticated ? dashboardPath : '/login'" class="rounded p-3" @click="mobileMenuOpen = false">{{ isAuthenticated ? '进入控制台' : '登录' }}</router-link>
      </nav>
    </header>

    <!-- Main Content -->
    <main class="relative z-10 flex-1">
      <!-- ===== 1. Dynamic Honeycomb Constellation Hero Section ===== -->
      <section class="omnihive-hero-section relative overflow-hidden border-b">
        <div class="hero-grid mx-auto max-w-[1360px] px-4 sm:px-6 lg:px-8">
          <!-- Right / Background: SVG Elliptical Orbit Honeycomb Constellation Network (#hiveNet) -->
          <svg
            id="hiveNet"
            ref="hiveNet"
            class="pointer-events-auto select-none"
            viewBox="120 -40 1320 640"
            fill="none"
            preserveAspectRatio="xMidYMid meet"
            role="group"
            aria-label="蜂巢网络示意：上游模型花丛、请求蜜蜂与调度中枢"
          >
            <defs>
              <radialGradient id="hiveGlow">
                <stop offset="0" stop-color="var(--brand, #f59e0b)" stop-opacity=".22" />
                <stop offset="1" stop-color="var(--brand, #f59e0b)" stop-opacity="0" />
              </radialGradient>
            </defs>
            <circle cx="820" cy="280" r="250" fill="url(#hiveGlow)" />

            <!-- Elliptical Orbits -->
            <ellipse cx="820" cy="280" rx="470" ry="205" stroke="currentColor" stroke-opacity="0.14" stroke-width="1" />
            <ellipse cx="820" cy="280" rx="340" ry="140" stroke="currentColor" stroke-opacity="0.18" stroke-width="1" />
            <ellipse
              class="dash-flow"
              cx="820"
              cy="280"
              rx="565"
              ry="262"
              stroke="currentColor"
              stroke-opacity="0.15"
              stroke-width="1"
              stroke-dasharray="3 9"
            />

            <!-- Satellite Flower Cluster Nodes (Claude, GPT, Gemini, Grok, DeepSeek, Ollama) -->
            <g
              v-for="flower in flowerNodes"
              :key="flower.name"
              v-show="!hiddenBees.includes(flower.key)"
              class="flower hive-node"
              :class="{ 'is-sim-target': activeFlowerIndex === flower.index }"
              :data-name="flower.name"
              :data-prov="flower.provider"
              :data-flower="flower.index"
              tabindex="0"
              role="button"
              :aria-label="`${flower.name} 花丛 · ${flower.provider}`"
              :style="{
                transformOrigin: `${flower.x}px ${flower.y}px`,
                animationDelay: `${flower.index * 0.4}s`,
                cursor: 'pointer'
              }"
              @click="selectFlowerNode(flower)"
              @keydown.enter.prevent="selectFlowerNode(flower)"
              @keydown.space.prevent="selectFlowerNode(flower)"
              @mouseenter="showHiveTip($event, `${flower.name} · ${flower.provider} · 花丛节点活跃 · 可调度`)"
              @focus="showHiveTip($event, `${flower.name} · ${flower.provider} · 花丛节点活跃 · 可调度`)"
              @mouseleave="hiveTip = null"
              @blur="hiveTip = null"
            >
              <circle
                :cx="flower.x"
                :cy="flower.y"
                :r="activeFlowerIndex === flower.index ? 42 : 30"
                :fill="flower.color"
                :opacity="activeFlowerIndex === flower.index ? 0.28 : 0.13"
              />
              <ellipse
                v-for="angle in [0, 60, 120, 180, 240, 300]"
                :key="angle"
                :cx="flower.x"
                :cy="flower.y - 11"
                rx="6"
                ry="11"
                :fill="flower.color"
                :opacity="activeFlowerIndex === flower.index ? 0.85 : 0.48"
                :transform="`rotate(${angle} ${flower.x} ${flower.y})`"
              />
              <circle :cx="flower.x" :cy="flower.y" r="7.5" :fill="flower.color" opacity=".9" />
              <circle :cx="flower.x" :cy="flower.y" r="3" fill="var(--card, #ffffff)" />
              <text
                :x="flower.x"
                :y="flower.y + 34"
                text-anchor="middle"
                font-size="12"
                font-weight="600"
                fill="currentColor"
                opacity="0.72"
              >
                {{ flower.name }}
              </text>
            </g>

            <!-- Orbiting Bees Carrying Nectar (Tokens) -->
            <g
              v-for="(bee, idx) in orbitingBees"
              :key="bee.key"
              v-show="!hiddenBees.includes(bee.key)"
              class="bee-orbit"
              :data-model="bee.key"
            >
              <g transform="scale(.86)">
                <g class="bee-body" :style="{ animationDelay: `${idx * 0.18}s` }" v-html="beeGlyph(bee.key)"></g>
              </g>
              <animateMotion
                :dur="`${bee.dur}s`"
                :begin="`-${(idx * 2.1).toFixed(1)}s`"
                repeatCount="indefinite"
                rotate="auto"
                :path="bee.path"
              />
            </g>

            <!-- Central Honeycomb Core (#hiveCore): OmniHive Gateway -->
            <g id="hiveCore" class="floaty" style="transform-origin: 820px 280px">
              <circle
                cx="820"
                cy="280"
                r="64"
                fill="var(--card, #ffffff)"
                stroke="var(--brand, #f59e0b)"
                stroke-opacity="0.45"
                stroke-width="1.5"
                class="pulse-ring"
              />
              <path
                v-for="(cell, idx) in hiveCells"
                :key="idx"
                class="hive-cell"
                :class="{ lit: litCells.includes(idx) || idx === 0 }"
                :data-cell="idx"
                :d="`M ${820 + cell.dx} ${280 + cell.dy - 11} l 9.5 5.5 v 11 l -9.5 5.5 l -9.5 -5.5 v -11 Z`"
                :fill="litCells.includes(idx) || idx === 0 ? 'var(--brand, #f59e0b)' : 'rgba(245, 158, 11, 0.16)'"
                stroke="var(--brand, #f59e0b)"
                stroke-opacity="0.55"
                stroke-width="1"
                tabindex="0"
                role="button"
                :aria-label="cell.label"
                @keydown.enter.prevent="highlightHiveCell(idx, cell.label)"
                @keydown.space.prevent="highlightHiveCell(idx, cell.label)"
                @mouseenter="showHiveTip($event, cell.label)"
                @focus="showHiveTip($event, cell.label)"
                @mouseleave="hiveTip = null"
                @blur="hiveTip = null"
                @click="highlightHiveCell(idx, cell.label)"
              />
              <circle cx="820" cy="280" r="4" fill="var(--card, #ffffff)" />
            </g>
            <text
              x="820"
              y="370"
              text-anchor="middle"
              font-size="12"
              font-weight="600"
              letter-spacing="2"
              fill="currentColor"
              opacity="0.55"
            >
              OMNIHIVE · 调度中枢
            </text>

            <!-- Beekeeper (User) Node -->
            <g class="hive-node" tabindex="0" role="button" aria-label="你 · 一把密钥" @click="navigate('/keys')" @keydown.enter.prevent="navigate('/keys')" @keydown.space.prevent="navigate('/keys')">
              <circle cx="180" cy="280" r="21" fill="var(--card, #ffffff)" stroke="var(--brand, #f59e0b)" stroke-opacity="0.5" />
              <circle cx="180" cy="274" r="5" fill="var(--brand, #f59e0b)" />
              <path d="M170 288q10-9 20 0" stroke="var(--brand, #f59e0b)" stroke-width="2" fill="none" />
              <text x="180" y="318" text-anchor="middle" font-size="11" fill="currentColor" opacity="0.65">
                你 · 一把密钥
              </text>
            </g>

            <!-- Request & Response Trajectories -->
            <path
              d="M 202 280 Q 500 245 754 280"
              stroke="var(--brand, #f59e0b)"
              stroke-width="1.8"
              stroke-dasharray="4 6"
              fill="none"
              class="dash-flow"
              opacity=".75"
            />
            <text x="530" y="224" font-size="11" font-weight="600" fill="var(--brand, #f59e0b)">
              请求 · prompt 花蜜 →
            </text>
            <text x="530" y="356" font-size="11" font-weight="600" fill="var(--ok, #16a34a)">
              ← 返回 · completion 酿蜜
            </text>
            <path
              d="M 754 310 Q 500 350 202 310"
              stroke="var(--ok, #16a34a)"
              stroke-width="1.6"
              stroke-dasharray="4 6"
              fill="none"
              class="dash-flow"
              opacity=".55"
            />
          </svg>
          <div v-show="hiveTip" id="hiveTip" class="omnihive-card pointer-events-none absolute z-20 px-3 py-2 text-xs" role="tooltip" :style="{ left: `${hiveTip?.x ?? 0}px`, top: `${hiveTip?.y ?? 0}px` }">{{ hiveTip?.text }}</div>

          <!-- Interactive Bee Legend & Pause Control -->
          <div id="beeLegend" class="absolute bottom-4 right-6 z-10 hidden max-w-[420px] flex-wrap justify-end gap-1.5 lg:flex">
            <button
              v-for="bee in orbitingBees.slice(0, 10)"
              :key="bee.key"
              type="button"
              class="legend-chip"
              :class="{ off: hiddenBees.includes(bee.key) }"
              :aria-pressed="!hiddenBees.includes(bee.key)"
              @click="toggleBeeFilter(bee.key)"
            >
              <i :style="{ backgroundColor: bee.c1 }"></i>
              <span>{{ bee.name }}</span>
            </button>
            <button
              type="button"
              class="legend-chip"
              :aria-pressed="beePaused"
              @click="toggleBeePause"
            >
              {{ beePaused ? '▶ 继续动画' : '⏸ 暂停' }}
            </button>
          </div>

          <!-- Left Column: Hero Copy & CTAs -->
          <div class="hero-copy relative z-10 max-w-2xl py-12 lg:max-w-[46%] lg:py-20">
            <!-- Eyebrow Pill -->
            <div class="mb-5 inline-flex items-center gap-2.5 rounded-full border px-3.5 py-1 text-xs font-semibold tracking-[0.16em]" style="border-color: var(--border, #e9e9ee); background: var(--card, #ffffff)">
              <span class="pulse-dot pulse-ok"></span>
              <span>OMNIHIVE · 中立 AI 网关</span>
            </div>

            <!-- Main Title (preserves siteName + prototype headline) -->
            <h1 class="text-4xl font-bold leading-[1.06] tracking-tight sm:text-5xl lg:text-6xl">
              每个工作流，<br /><span class="brand-gradient">都值得整个蜂巢。</span>
            </h1>
            <p class="mt-4 text-sm font-semibold">{{ siteName }} · {{ siteSubtitle }}</p>

            <!-- Subtitle / Proposition -->
            <p class="mt-5 max-w-xl text-base leading-relaxed opacity-75 sm:text-lg">
              在一个中立的中转站上运行你所有的 AI 负载——以每一块钱买到最聪明的智力。Claude、GPT、Gemini 由蜂巢自动调度、自动容灾。
            </p>

            <!-- Primary & Secondary CTA Buttons + Simulate Dispatch Button -->
            <div class="mt-8 flex flex-wrap items-center gap-3">
              <router-link
                :to="isAuthenticated ? '/keys' : '/register'"
                class="omnihive-btn-primary px-6 py-3 text-sm font-semibold shadow-sm"
              >
                <span>获取 API Key →</span>
              </router-link>

              <router-link
                v-if="showModelPlazaEntry"
                to="/model-plaza"
                class="omnihive-btn-outline px-5 py-3 text-sm font-semibold"
              >
                <span>浏览模型</span>
                <span class="font-mono text-xs opacity-60">{}</span>
              </router-link>
              <a
                v-else
                href="#ecosystem"
                class="omnihive-btn-outline px-5 py-3 text-sm font-semibold"
              >
                <span>浏览模型生态</span>
                <span class="font-mono text-xs opacity-60">{}</span>
              </a>

              <button
                id="heroSimulateBtn"
                type="button"
                class="omnihive-btn-outline px-4 py-3 text-sm font-semibold"
                :disabled="simRunning"
                title="查看蜂群调度与采蜜 Token 过程"
                @click="simulateRequest"
              >
                <span class="pulse-dot pulse-ok"></span>
                <span>{{ simRunning ? '蜂群调度中…' : '查看请求调度 ⚡' }}</span>
              </button>
            </div>

            <!-- Live Simulation Feedback Banner -->
            <div
              v-if="simFeedback"
              class="mt-4 flex items-center gap-2.5 rounded-xl border px-3.5 py-2.5 text-xs font-medium transition-all"
              style="border-color: color-mix(in srgb, var(--brand, #f59e0b) 45%, var(--border, #e9e9ee)); background: color-mix(in srgb, var(--brand, #f59e0b) 10%, var(--card, #ffffff))"
              role="status"
              aria-live="polite"
            >
              <span class="pulse-dot pulse-ok"></span>
              <span class="font-mono">{{ simFeedback }}</span>
            </div>

            <!-- Three Cardless Large-Number Metrics Row (tabular-nums) -->
            <div class="mt-10 grid grid-cols-3 gap-6 border-t pt-6 sm:gap-10" style="border-color: var(--border, #e9e9ee)">
              <div>
                <div class="stat-num text-2xl font-bold tabular-nums sm:text-3xl">
                  06
                </div>
                <div class="mt-0.5 text-xs opacity-60 sm:text-sm">客户端接入指南</div>
              </div>
              <div>
                <div class="stat-num text-2xl font-bold tabular-nums sm:text-3xl">
                  03
                </div>
                <div class="mt-0.5 text-xs opacity-60 sm:text-sm">原生协议体系</div>
              </div>
              <div>
                <div class="stat-num text-2xl font-bold tabular-nums sm:text-3xl">
                  01
                </div>
                <div class="mt-0.5 text-xs opacity-60 sm:text-sm">统一密钥入口</div>
              </div>
            </div>
            <p class="mt-3 text-[11px] opacity-50">OpenAI · Anthropic · Gemini，具体模型与能力以分组配置为准。</p>
            <div id="mobileHiveFlow" class="omnihive-card mt-5 p-4 lg:hidden">
              <div class="flex items-center justify-between text-sm"><b>请求如何穿过蜂巢</b><button :aria-pressed="beePaused" @click="toggleBeePause">{{ beePaused ? '继续动画' : '暂停动画' }}</button></div>
              <div class="mt-3 flex justify-between text-xs"><span>你 · 请求</span><span>→</span><span>蜂巢 · 调度</span><span>→</span><span>上游模型</span></div>
              <div class="mobile-bees mt-3 flex justify-between overflow-hidden">
                <div v-for="(key, index) in ['claude', 'gpt', 'gemini', 'deepseek']" :key="key" class="mobile-bee text-center" :style="{ animationDelay: `-${index * 2}s` }">
                  <svg width="48" height="40" viewBox="-38 -34 82 66" fill="none" aria-hidden="true" v-html="beeGlyph(key)"></svg>
                  <span class="text-[10px]">{{ BEE_SPECS[key].name }}</span>
                </div>
              </div>
              <p class="mt-2 text-xs opacity-65">蜜蜂把请求送到模型，再将返回的 Token 带回蜂巢。</p>
            </div>
          </div>
        </div>
      </section>

      <!-- ===== Main Container (1200px) ===== -->
      <div class="mx-auto max-w-[1200px] px-4 sm:px-6">
        <!-- Client Ecosystem Strip -->
        <div class="flex flex-wrap items-center justify-center gap-x-10 gap-y-3 border-b py-7 text-sm font-semibold opacity-55 sm:text-base" style="border-color: var(--border, #e9e9ee)">
          <span
            v-for="client in ecosystemClients"
            :key="client"
            class="cursor-default transition-opacity hover:opacity-100"
          >
            {{ client }}
          </span>
        </div>

        <!-- Honeycomb Worldview Card -->
        <section class="omnihive-card hive-dots my-12 p-6 sm:p-8">
          <div class="grid items-start gap-6 lg:grid-cols-[1fr_2.2fr]">
            <div>
              <div class="mb-2 flex items-center gap-2.5">
                <span class="brand-hairline"></span>
                <span class="text-xs font-semibold uppercase tracking-[0.18em] opacity-60">蜂巢是怎么运转的</span>
              </div>
              <h2 class="text-2xl font-bold leading-snug">
                你不必认识每一朵花，<br />蜂巢替你安排蜜蜂。
              </h2>
              <div class="mt-4 flex flex-wrap gap-2">
                <span class="omnihive-tag-chip">
                  <Icon name="swap" size="xs" />
                  {{ t('home.tags.subscriptionToApi') }}
                </span>
                <span class="omnihive-tag-chip">
                  <Icon name="shield" size="xs" />
                  {{ t('home.tags.stickySession') }}
                </span>
                <span class="omnihive-tag-chip">
                  <Icon name="chart" size="xs" />
                  {{ t('home.tags.realtimeBilling') }}
                </span>
              </div>
            </div>
            <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
              <div
                v-for="pillar in worldviewPillars"
                :key="pillar.title"
                class="omnihive-card p-4 transition-transform hover:-translate-y-0.5"
              >
                <div class="mb-1.5 text-xl">{{ pillar.icon }}</div>
                <div class="text-sm font-semibold">{{ pillar.title }}</div>
                <p class="mt-1.5 text-xs leading-relaxed opacity-65">{{ pillar.desc }}</p>
              </div>
            </div>
          </div>
        </section>

        <section id="swarm" class="mb-14">
          <div class="mb-4 flex flex-wrap items-end justify-between gap-4">
            <div><div class="pub-eyebrow mb-2">THE SWARM · 蜂群全家福</div><h2 class="text-2xl font-bold">主流模型，都坐上蜂巢了。</h2><p class="mt-2 text-sm opacity-60">每只蜜蜂按模型性格长成不同模样——翼型、条纹、触角、头饰都不一样。点开看它的档案。</p></div>
            <input v-model="beeSearch" type="search" class="portal-input max-w-[220px]" placeholder="搜索模型蜜蜂…" aria-label="搜索模型蜜蜂" />
          </div>
          <div class="grid grid-cols-2 gap-3 sm:grid-cols-4 lg:grid-cols-8">
            <button v-for="[key, bee] in filteredBees" :key="key" class="omnihive-card bee-card p-3 text-center" @click="beeDetail(key)">
              <svg width="100%" height="66" viewBox="-38 -34 82 66" fill="none" aria-hidden="true"><g class="bee-body" v-html="beeGlyph(key)"></g></svg>
              <div class="truncate text-xs font-semibold">{{ bee.name }}</div><div class="mt-1 truncate text-[10px] opacity-55">{{ bee.prov }}</div>
            </button>
          </div>
          <p v-if="!filteredBees.length" class="py-8 text-center text-sm opacity-60" role="status">未找到匹配的蜜蜂，请换个关键词。</p>
          <p class="mt-3 text-xs opacity-50">✦ 16 只蜜蜂同巢待命 · 蜂巢按健康度、延迟与配额水位决定派谁出勤</p>
        </section>

        <!-- ===== 2. Interactive Quickstart Demo (with .terminal-container) ===== -->
        <section id="rankings" class="pub-section" aria-labelledby="rankings-title">
          <div class="mb-6 flex flex-wrap items-end justify-between gap-4">
            <div><p class="mb-2 font-mono text-xs text-[var(--muted-foreground)]">OmniHive / Network intelligence</p><h2 id="rankings-title" class="text-2xl font-bold sm:text-3xl">实时算力排行榜</h2><p class="mt-2 text-sm text-[var(--muted-foreground)]">从 Token 流量、可用性与成本，选择适合工作负载的模型。</p></div>
            <button type="button" class="omnihive-btn-outline px-3 py-2 text-xs" :disabled="rankingsLoading" @click="loadRankings">{{ rankingsLoading ? '正在同步…' : '刷新排行' }}</button>
          </div>
          <div class="mb-4 flex gap-5 overflow-x-auto border-b border-[var(--border)]" role="group" aria-label="排行榜维度">
            <button v-for="tab in rankingTabs" :key="tab.key" type="button" class="shrink-0 border-b-2 py-3 text-sm" :class="rankingTab === tab.key ? 'border-[var(--brand)] text-[var(--brand-text)]' : 'border-transparent text-[var(--muted-foreground)]'" :aria-pressed="rankingTab === tab.key" @click="rankingTab = tab.key">{{ tab.label }}</button>
          </div>
          <p class="mb-3 text-xs text-[var(--muted-foreground)]" role="status">{{ rankingStatus }}</p>
          <div class="overflow-x-auto rounded-xl border border-[var(--border)] bg-[var(--card)]">
            <table class="w-full min-w-[570px] text-left text-sm">
              <thead class="border-b border-[var(--border)] bg-[var(--surface-2)] text-xs text-[var(--muted-foreground)]"><tr><th scope="col" class="px-5 py-3">#</th><th scope="col" class="px-5 py-3">{{ rankingTab === 'channels' ? '供应商' : '模型' }}</th><th scope="col" class="px-5 py-3 text-right">{{ rankingTab === 'models' ? 'Token Volume' : rankingTab === 'channels' ? 'Uptime' : 'Cost Efficiency' }}</th><th scope="col" class="px-5 py-3 text-right">{{ rankingTab === 'channels' ? 'P95 / 变化' : '变化' }}</th><th scope="col" class="px-5 py-3 text-right">趋势</th></tr></thead>
              <tbody><tr v-for="(row, index) in rankingData[rankingTab]" :key="`${row.name}-${index}`" class="border-b border-[var(--border)] last:border-0 hover:bg-[var(--surface-2)]">
                <td class="px-5 py-4 font-mono text-xs tabular-nums text-[var(--muted-foreground)]">{{ String(index + 1).padStart(2, '0') }}</td>
                <th scope="row" class="px-5 py-4 font-medium">{{ row.name }}<span class="mt-1 block text-xs font-normal text-[var(--muted-foreground)]">{{ row.provider }}</span></th>
                <td class="px-5 py-4 text-right font-mono tabular-nums">{{ row.metric }}</td>
                <td class="px-5 py-4 text-right font-mono tabular-nums text-[var(--brand-text)]">{{ row.delta || '—' }}</td>
                <td class="px-5 py-4"><svg v-if="row.history.length > 1" class="ml-auto h-7 w-24 text-[var(--brand-text)]" viewBox="0 0 100 28" role="img" :aria-label="`${row.name} ${rankingsLive && !rankingsReference ? '历史趋势' : '参考趋势'}`"><polyline :points="sparkline(row.history)" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg><span v-else class="block text-right text-xs text-[var(--muted-foreground)]">暂无趋势</span></td>
              </tr><tr v-if="!rankingData[rankingTab].length"><td colspan="5" class="p-8 text-center text-[var(--muted-foreground)]">当前统计窗口暂无数据。</td></tr></tbody>
            </table>
          </div>
        </section>
        <section id="quickstart" class="pub-section">
          <div class="grid items-center gap-10 lg:grid-cols-12">
            <!-- Left: SDK Segmented Selector & Highlights -->
            <div class="lg:col-span-5">
              <div class="mb-3 flex items-center gap-2.5">
                <span class="brand-hairline"></span>
                <span class="text-xs font-semibold uppercase tracking-[0.18em] opacity-60">三行配置，接入全部模型</span>
              </div>
              <h2 class="text-3xl font-bold leading-tight sm:text-4xl">
                改一个 Base URL，<br />其他都不用动。
              </h2>
              <p class="mt-4 text-sm leading-relaxed opacity-70">
                原生兼容 OpenAI、Anthropic、Gemini 与 Claude Code / Codex 协议。切换下方接入方式，一键复制即可直连蜂巢中枢。
              </p>

              <!-- Segmented Control -->
              <div class="segmented mt-6 flex-wrap" role="tablist" aria-label="接入代码语言切换">
                <button
                  v-for="(tab, index) in sdkTabs"
                  :key="tab.key"
                  :id="`sdk-tab-${tab.key}`"
                  type="button"
                  role="tab"
                  aria-controls="sdk-panel"
                  :tabindex="activeSdkTab === tab.key ? 0 : -1"
                  :aria-selected="activeSdkTab === tab.key"
                  :class="{ on: activeSdkTab === tab.key }"
                  @click="activeSdkTab = tab.key; sdkCopied = false; copyError = ''"
                  @keydown="moveSdkTab($event, index)"
                >
                  <span class="mr-1 font-mono text-xs">[{{ String(index + 1).padStart(2, '0') }}]</span>{{ tab.label }}
                </button>
              </div>

              <div class="mt-6 space-y-2.5 text-sm">
                <div class="flex items-center gap-2">
                  <span class="font-bold" style="color: var(--ok, #16a34a)">✓</span>
                  <span class="opacity-80">无需改动现有 SDK 与代码</span>
                </div>
                <div class="flex items-center gap-2">
                  <span class="font-bold" style="color: var(--ok, #16a34a)">✓</span>
                  <span class="opacity-80">按量计费，Token 级成本明细</span>
                </div>
                <div class="flex items-center gap-2">
                  <span class="font-bold" style="color: var(--ok, #16a34a)">✓</span>
                  <span class="opacity-80">上游故障自动切换，具体能力以渠道配置为准</span>
                </div>
              </div>
            </div>

            <!-- Right: Upgraded .terminal-container (preserves HomeView.compact.spec.ts contract) -->
            <div class="lg:col-span-7">
              <div id="sdk-panel" class="terminal-container w-full" role="tabpanel" :aria-labelledby="`sdk-tab-${activeSdkTab}`">
                <div class="terminal-window w-full">
                  <!-- Window Header -->
                  <div class="terminal-header justify-between">
                    <div class="flex items-center gap-3">
                      <div class="terminal-buttons">
                        <span class="btn-close"></span>
                        <span class="btn-minimize"></span>
                        <span class="btn-maximize"></span>
                      </div>
                      <span class="font-mono text-xs text-slate-400">{{ currentSdkSnippet.file }}</span>
                    </div>
                    <div class="flex items-center gap-2">
                      <button
                        type="button"
                        class="rounded-md border border-slate-700 bg-slate-800/90 px-2.5 py-1 font-mono text-xs text-slate-200 transition hover:border-amber-500/60 hover:text-white"
                        @click="copySdkCode"
                      >
                        {{ sdkCopied ? '✓ 已复制' : '⧉ 复制代码' }}
                      </button>
                    </div>
                  </div>

                  <!-- Terminal Body -->
                  <div class="terminal-body space-y-3">
                    <pre class="overflow-x-auto font-mono text-xs leading-relaxed text-slate-200 sm:text-sm"><code>{{ currentSdkSnippet.code }}</code></pre>

                    <!-- Live Dispatch Status Trace Bar inside Terminal -->
                    <div class="mt-4 rounded-lg border border-slate-800 bg-slate-950/80 p-3 font-mono text-xs">
                      <div class="flex flex-wrap items-center justify-between gap-2 text-slate-400">
                        <span># 调度流程示意 · OmniHive → <strong class="text-amber-400">{{ simLastGroup }}</strong></span>
                        <span class="text-emerald-400">200 OK · {{ simLatency }}ms</span>
                      </div>
                      <div class="mt-1.5 flex flex-wrap items-center gap-2 text-slate-300">
                        <span class="code-success">200 OK</span>
                        <span class="code-response">{ "model": "{{ simLastModel }}", "tokens": {{ simLastTokens }}, "cost": "{{ simLastCost }}" }</span>
                        <span class="cursor"></span>
                      </div>
                    </div>
                  </div>
                </div>
                <p v-if="copyError" class="mt-2 text-xs text-red-600" role="status">{{ copyError }}</p>
                <p v-else-if="sdkCopied" class="mt-2 text-xs text-[var(--brand-text)]" role="status">配置已复制。</p>
                <p class="mt-3 text-xs text-[var(--muted-foreground)]">{{ sdkRoot ? '请替换模型 ID，并通过环境变量配置您的 API Key。' : '本站尚未配置公开网关地址；请先向管理员确认，再替换 YOUR_GATEWAY_ROOT。' }}</p>
              </div>
            </div>
          </div>
        </section>

        <!-- ===== 3. Pain Points -> Honeycomb Solutions & Architecture Matrix ===== -->
        <section id="architecture" class="pub-section">
          <div class="mb-8 flex flex-wrap items-end justify-between gap-4">
            <div>
              <div class="pub-eyebrow mb-2">WHY OMNIHIVE · 痛点终结者</div>
              <h2 class="text-3xl font-bold">别再跟一堆散乱的订阅账号较劲了</h2>
              <p class="mt-1.5 text-sm opacity-65">从个人开发者到百人工程团队，蜂巢将散落的账号转化为高可用算力池。</p>
            </div>
          </div>

          <!-- 4 Pain -> Solution Cards -->
          <div class="mb-10 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <div
              v-for="item in painPoints"
              :key="item.painTitle"
              class="omnihive-card pain-card group p-5 transition-all hover:-translate-y-0.5"
              tabindex="0"
            >
              <div class="flex items-center justify-between text-xs font-semibold opacity-55">
                <span>传统痛点</span>
                <span>→ 蜂巢方案</span>
              </div>
              <div class="mt-2 text-base font-bold">{{ item.painTitle }}</div>
              <p class="pain-desc mt-1 text-xs opacity-60">{{ item.painDesc }}</p>
              <div class="pain-fix mt-4 border-t pt-3 text-xs font-semibold" style="border-color: var(--border, #e9e9ee); color: var(--brand-text, #92610a)">
                ✦ {{ item.solution }}
              </div>
            </div>
          </div>

          <!-- Comparison Matrix Table -->
          <h2 class="mb-5 text-2xl font-bold">为什么是蜂巢</h2>
          <div class="omnihive-card overflow-x-auto">
            <table class="w-full text-left text-sm">
              <thead>
                <tr class="border-b text-xs uppercase tracking-wider opacity-60" style="border-color: var(--border, #e9e9ee); background: var(--surface-2, #f4f4f5)">
                  <th class="px-5 py-3.5 font-semibold">对比维度</th>
                  <th class="px-5 py-3.5 font-semibold">各家官方单点订阅</th>
                  <th class="px-5 py-3.5 font-semibold">OmniHive 算力蜂巢</th>
                </tr>
              </thead>
              <tbody class="divide-y" style="border-color: var(--border, #e9e9ee)">
                <tr v-for="row in comparisonRows" :key="row.dim" class="transition-colors hover:bg-amber-500/[0.04]">
                  <td class="px-5 py-3.5 font-medium">{{ row.dim }}</td>
                  <td class="px-5 py-3.5 opacity-60">{{ row.official }}</td>
                  <td class="px-5 py-3.5 font-semibold">
                    <span class="mr-1.5" style="color: var(--ok, #16a34a)">✓</span>{{ row.hive }}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>

        <section id="faq" class="pub-section">
          <h2 class="mb-5 text-2xl font-bold">常见问题</h2>
          <div id="faqList" class="space-y-2">
            <details v-for="faq in faqItems" :key="faq.q" class="omnihive-card group p-4">
              <summary class="flex cursor-pointer list-none items-center justify-between font-medium"><span>{{ faq.q }}</span><span class="ml-2 opacity-40 transition-transform group-open:rotate-180">⌄</span></summary>
              <p class="mt-3 text-sm leading-relaxed opacity-70">{{ faq.a }}</p>
            </details>
          </div>
        </section>

        <!-- ===== 5. Supported Model Ecosystem & Capabilities Grid ===== -->
        <section id="ecosystem" class="pub-section">
          <div class="mb-6 flex flex-wrap items-end justify-between gap-4">
            <div>
              <div class="pub-eyebrow mb-2">ONE API FOR EVERY MODEL · 模型生态</div>
              <h2 class="text-3xl font-bold">一把密钥，多种模型，全协议兼容。</h2>
              <p class="mt-1.5 text-sm opacity-65">实时同步 40+ 供应商模型调用趋势，完整定价与参数请查看模型广场。</p>
            </div>
            <router-link
              v-if="showModelPlazaEntry"
              to="/model-plaza"
              class="omnihive-btn-outline px-4 py-2 text-xs font-semibold"
            >
              进入模型广场查看实时倍率 →
            </router-link>
          </div>

          <!-- Category Filter Tabs -->
          <div class="mb-5 flex flex-wrap gap-5 border-b text-sm" style="border-color: var(--border, #e9e9ee)">
            <button
              v-for="cat in ecosystemCategories"
              :key="cat.key"
              type="button"
              class="-mb-px whitespace-nowrap border-b-2 pb-2.5 transition-colors"
              :class="activeCategory === cat.key ? 'font-semibold opacity-100' : 'border-transparent opacity-55 hover:opacity-85'"
              :style="activeCategory === cat.key ? { borderColor: 'var(--brand, #f59e0b)' } : undefined"
              :aria-pressed="activeCategory === cat.key"
              @click="activeCategory = cat.key"
            >
              {{ cat.label }} <span class="ml-1 font-mono text-xs opacity-60">{{ cat.count }}</span>
            </button>
          </div>

          <!-- Model Cards Grid -->
          <div class="grid gap-3.5 sm:grid-cols-2 lg:grid-cols-3">
            <button
              v-for="m in filteredEcosystemModels"
              :key="m.name"
              class="omnihive-card flex items-center gap-3.5 p-4 transition-all hover:-translate-y-0.5"
              @click="navigate(`/model-plaza?search=${encodeURIComponent(m.name)}`)"
            >
              <span
                class="grid h-9 w-9 shrink-0 place-items-center rounded-xl text-xs font-bold text-white shadow-sm"
                :style="{ backgroundColor: m.color }"
              >
                {{ m.badge }}
              </span>
              <div class="min-w-0 flex-1">
                <div class="flex items-center gap-1.5">
                  <span class="truncate text-sm font-semibold">{{ m.name }}</span>
                  <span class="rounded bg-amber-500/15 px-1.5 py-0.5 font-mono text-[10px] font-medium text-amber-700 dark:text-amber-300">
                    {{ m.context }}
                  </span>
                </div>
                <div class="mt-0.5 truncate text-xs opacity-60">weekly tokens · 7d</div>
              </div>
              <div class="text-right font-mono tabular-nums">
                <div class="text-sm font-bold">{{ m.tokens }}</div>
                <div class="text-[11px]" :style="{ color: m.trend.startsWith('-') ? 'var(--bad, #dc2626)' : 'var(--ok, #16a34a)' }">{{ m.trend }}</div>
              </div>
            </button>
          </div>
        </section>

        <section id="routing" class="pub-section grid items-center gap-12 lg:grid-cols-2">
          <div>
            <div class="pub-eyebrow mb-3">Better, faster, cheaper</div><h2 class="text-3xl font-bold leading-tight">更快、更省、<br />还全都拿得到。</h2>
            <p class="mt-4 text-sm leading-relaxed opacity-70">选择路由偏好，平衡模型成本与响应速度。登录后可将偏好保存到策略中心，实际费用以用量账单为准。</p>
            <div id="routeSeg" class="segmented mt-5 flex-wrap" role="group" aria-label="路由策略">
              <button v-for="strategy in routingStrategies" :key="strategy.key" :disabled="policyBusy" :aria-pressed="activeRoutingKey === strategy.key" :class="{ on: activeRoutingKey === strategy.key }" @click="saveHomePolicy({ strategy_mode: strategy.key }, `${strategy.label}，路由策略已更新。`)">{{ strategy.label }}</button>
            </div>
            <button class="mt-4 block text-sm" @click="openMethodology">对比方法说明 →</button>
          </div>
          <div class="omnihive-card p-5">
            <div class="mb-4 flex flex-wrap justify-between gap-2 text-xs opacity-60"><span>模型分布 · 实时</span><span>预计节省 <b id="routeSave" style="color:var(--ok, #16a34a)">{{ currentRoutingStrategy.saving }}</b></span></div>
            <div id="routeRows" class="space-y-3" aria-live="polite">
              <div v-for="item in currentRoutingStrategy.rows" :key="item.model" class="flex min-w-0 items-center gap-2 text-sm">
                <span class="h-2 w-2 shrink-0 rounded-full" :style="{ background: item.color }"></span><span class="min-w-0 flex-1 truncate">{{ item.model }}</span>
                <div class="portal-meter w-16 shrink-0 sm:w-24"><span :style="{ width: `${item.percent}%`, background: item.color }"></span></div><span class="w-9 text-right text-xs tabular-nums">{{ item.percent }}%</span>
              </div>
            </div>
            <div class="mt-4 flex justify-between border-t pt-3 text-xs"><span class="opacity-60">示例月度节省</span><b id="routeSave2" style="color:var(--ok, #16a34a)">{{ currentRoutingStrategy.saving }}</b></div>
          </div>
        </section>

        <section id="reliability" class="pub-section grid items-start gap-12 lg:grid-cols-2">
          <div>
            <div class="pub-eyebrow mb-4">Providers</div>
            <div class="space-y-3 text-sm">
              <div v-for="provider in providers" :key="provider.name" class="flex items-center gap-2">
                <span class="h-2 w-2 shrink-0 rounded-full" :style="{ background: provider.color }"></span>
                <span class="w-28 shrink-0 sm:w-32">{{ provider.name }}</span>
                <div class="flex flex-wrap gap-1">
                  <span v-for="index in provider.nodes" :key="index" class="prov-cell" :class="{ lit: index <= Math.ceil(provider.nodes * .7) }" :title="`${provider.name} 节点 ${index} · ${index <= Math.ceil(provider.nodes * .7) ? '健康' : '不可用'}（在线）`"></span>
                </div>
              </div>
              <div class="flex items-center gap-2 border-t pt-3">
                <span class="h-2 w-2 shrink-0 rounded-full" style="background:var(--brand, #f59e0b)"></span><b class="w-28 shrink-0 text-xs sm:w-32">Through OmniHive</b>
                <div class="flex flex-wrap gap-1"><span v-for="index in 16" :key="index" class="prov-cell lit" :title="`蜂巢节点 ${index} · 健康（在线）`"></span></div>
              </div>
              <p class="text-[11px] opacity-50">节点健康矩阵 · 实时遥测</p>
            </div>
          </div>
          <div>
            <div class="pub-eyebrow mb-3">Reliability</div><h2 class="text-3xl font-bold leading-tight">上游抖动，<br />请求照样到。</h2>
            <p class="mt-4 text-sm leading-relaxed opacity-70">默认按健康度自动故障切换。当某个模型触发限流，蜂巢会改派另一只蜜蜂；你也可以自带上游 Key 增加额外容量。</p>
            <button class="mt-4 text-sm" @click="navigate('/monitor')">查看容灾如何工作 →</button>
            <div class="omnihive-card mt-6 flex flex-wrap items-center gap-3 p-4">
              <span class="pulse-dot pulse-ok"></span><div class="flex-1 text-sm"><b>99.99% 可用性</b><p class="mt-1 text-xs opacity-60">过去 30 天 · 6 次毫秒级自动故障切换 · 0 请求掉线</p></div>
              <button class="omnihive-btn-outline px-3 py-2 text-xs" @click="navigate('/monitor')">渠道状态</button>
            </div>
          </div>
        </section>

        <section id="observability" class="pub-section grid items-center gap-12 lg:grid-cols-2">
          <div><div class="pub-eyebrow mb-2">Observability</div><h2 class="text-3xl font-bold leading-tight">用量、模型、预算，<br />都在一个地方。</h2>
            <p class="mt-4 text-sm leading-relaxed opacity-70">带上你自己的密钥，把已集成的所有模型与额度汇总到同一张仪表盘。每次请求的模型、Token 与费用明细都清清楚楚。</p>
            <button class="mt-4 text-sm" @click="navigate('/dashboard')">了解更多仪表盘 →</button>
          </div>
          <div class="omnihive-card p-5">
            <div class="grid grid-cols-3 gap-3 border-b pb-4 text-center">
              <div v-for="metric in [['请求数', '1.1M', '+11%'], ['Token', '8.48T', '+7%'], ['花费', '$1,383', '']]" :key="metric[0]">
                <div class="text-[11px] opacity-55">{{ metric[0] }}</div><div class="text-xl font-bold">{{ metric[1] }}</div><div class="text-[11px]" style="color:var(--ok, #16a34a)">{{ metric[2] }}</div>
              </div>
            </div>
            <div class="mt-3 text-[11px] opacity-55">最近 14 天 · 按模型调用堆叠</div>
            <div class="mt-3 flex h-24 items-end gap-1" role="img" aria-label="14 天模型用量，包含 GPT、Claude、Gemini 与其他模型">
              <div v-for="(height, index) in usageHeights" :key="index" class="usage-bar flex flex-1 flex-col-reverse overflow-hidden rounded-t-sm" :style="{ height: `${height}%`, animationDelay: `${index * 30}ms` }">
                <span v-for="(model, modelIndex) in usageModels" :key="model.name" :style="{ height: `${usageShares[index % 3][modelIndex]}%`, background: model.color }" :title="`第 ${index + 1} 天 · ${model.name} · ${usageShares[index % 3][modelIndex]}%`"></span>
              </div>
            </div>
            <div class="mt-3 flex flex-wrap gap-3 text-[11px] opacity-65"><span v-for="model in usageModels" :key="model.name" class="flex items-center gap-1"><i class="h-2 w-2 rounded-sm" :style="{ background: model.color }"></i>{{ model.name }}</span></div>
            <div class="mt-4 border-t pt-3"><div class="mb-2 flex justify-between text-xs"><span class="opacity-60">月度预算</span><span>$1,383 / $2,000</span></div>
              <div class="portal-meter" role="progressbar" aria-label="月度预算" :aria-valuenow="69" :aria-valuemin="0" :aria-valuemax="100"><span style="width:69%;background:var(--brand, #f59e0b)"></span></div><p class="mt-2 text-[11px] opacity-50">到达 {{ homePolicy.alert_threshold_percent }}% 时提醒</p>
            </div>
          </div>
        </section>

        <section id="transparency" class="pub-section grid items-start gap-12 lg:grid-cols-2">
          <div class="omnihive-card p-5">
            <div class="mb-4 flex justify-between text-sm"><b>策略偏好中心</b><span class="text-xs opacity-60">{{ policySynced ? '偏好已保存 · 账户级预设' : '路由与合规偏好' }}</span></div>
            <div class="space-y-4">
              <div v-for="policy in policies" :key="policy.label" class="flex items-center justify-between border-b pb-3 text-sm">
                <span>{{ policy.label }}</span>
                <button type="button" class="policy-switch" :class="{ on: policy.enabled }" role="switch" :aria-label="policy.label" :aria-checked="policy.enabled" :disabled="policyBusy" @click="togglePolicy(policy)"><span></span></button>
              </div>
              <div class="flex flex-wrap items-center justify-between gap-2 border-b pb-3 text-sm"><span>处理区域</span><div class="flex flex-wrap gap-1" role="group" aria-label="处理区域"><button v-for="region in policyRegions" :key="region.value" type="button" class="rounded-full border px-2 py-1 text-xs" :class="{ 'bg-amber-100 text-amber-900': homePolicy.region_preference === region.value }" :aria-pressed="homePolicy.region_preference === region.value" :disabled="policyBusy" @click="saveHomePolicy({ region_preference: region.value }, `处理区域已更新为 ${region.label}。`)">{{ region.label }}</button></div></div>
              <div class="flex flex-wrap items-center justify-between gap-2 border-b pb-3 text-sm"><label for="policy-providers">供应商白名单</label><input id="policy-providers" v-model="providerWhitelist" :disabled="policyBusy" class="portal-input max-w-full sm:w-52" placeholder="供应商名称，逗号分隔；留空不限" @change="saveProviderWhitelist" /></div>
            </div>
            <p class="mt-3 text-[11px] opacity-55">登录后保存为账户路由偏好预设（不直接覆盖所选分组的底层强约束）；未登录时仅保留当前页面状态。</p><p class="mt-2 text-xs" role="status">{{ policyFeedback }}</p>
          </div>
          <div><div class="pub-eyebrow mb-2">Transparency</div><h2 class="text-3xl font-bold leading-tight">数据与合规，<br />逐项如实标注。</h2>
            <p class="mt-4 text-sm leading-relaxed opacity-70">集中配置数据留存、处理区域与供应商范围，结合操作审计管理企业合规要求。具体认证材料请联系站点管理员。</p>
            <div class="mt-5 space-y-3 text-sm"><div v-for="item in complianceItems" :key="item[0]" class="flex items-center gap-2"><span :style="{ color: item[2] }">✓</span><span class="flex-1">{{ item[0] }}</span><span class="text-[11px] opacity-55">{{ item[1] }}</span></div></div>
            <button v-if="isAdmin" class="mt-4 text-sm" @click="navigate('/admin/audit-logs')">查看平台审计日志 →</button>
            <button v-else-if="isAuthenticated" class="mt-4 text-sm" @click="navigate('/dashboard')">查看个人安全审计 →</button>
            <button v-else class="mt-4 text-sm" @click="navigate('/login')">登录查看安全审计 →</button>
          </div>
        </section>

        <section id="built-on" class="pub-section">
          <div class="mb-5 flex flex-wrap items-end justify-between gap-4"><div><div class="pub-eyebrow mb-2">Built on OmniHive</div><h2 class="text-3xl font-bold">蜂巢上长出来的东西。</h2><p class="mt-2 text-sm opacity-60">从编码 Agent 到无限画布，从个人工具到企业工作流。</p></div><button class="omnihive-btn-outline px-3 py-2 text-xs" @click="navigate('/model-plaza')">查看全部 →</button></div>
          <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            <button v-for="project in showcaseProjects" :key="project[0]" class="showcase-card rounded-xl p-5 text-left text-white" :style="{ background: project[2] }" @click="openShowcase(project)">
              <span class="text-lg font-bold">{{ project[0] }}</span><span class="mt-2 block text-xs opacity-70">{{ project[1] }}</span>
            </button>
          </div>
        </section>

        <section v-if="chatOpen" id="playground" ref="chatPanel" class="pub-section scroll-mt-24">
          <div class="omnihive-card p-5 sm:p-8">
            <div class="flex items-center justify-between"><h2 class="text-2xl font-bold">蜂巢在线对话测试台</h2><button aria-label="关闭对话测试台" @click="chatOpen = false">✕</button></div>
            <p class="my-4 text-sm opacity-60">通过蜂巢统一网关发送测试消息，验证目标模型的路由分组与响应延迟。</p>
            <label class="text-sm">模型<input v-model="chatModel" :disabled="chatBusy" class="portal-input mt-2" list="playground-models" placeholder="输入模型广场中的模型 ID" maxlength="200" /></label>
            <datalist id="playground-models"><option v-for="model in ['claude-sonnet-4-5', 'gpt-4o', 'gemini-2.5-pro', 'deepseek-chat']" :key="model" :value="model" /></datalist>
            <div class="my-4 max-h-80 space-y-3 overflow-y-auto" aria-live="polite">
              <p v-if="!chatMessages.length" class="text-sm opacity-60">输入一条消息，体验蜂巢对话。</p>
              <div v-for="message in chatMessages" :key="message.id" class="rounded-lg border p-3 text-sm"><b>{{ message.role }}</b><p class="mt-2 whitespace-pre-wrap break-words">{{ message.text }}</p><p v-if="message.routedGroup !== undefined" class="mt-2 text-xs opacity-60">路由分组：{{ message.routedGroup }} · 响应延迟：{{ message.latencyMs }} ms</p><button v-if="message.failed" :disabled="chatBusy" class="omnihive-btn-outline mt-3 px-3 py-1.5" @click="retryChat(message.id)">重试此消息</button></div>
            </div>
            <form @submit.prevent="sendChatMessage">
              <label for="chat-message" class="text-sm">消息</label><textarea id="chat-message" ref="chatInput" v-model="chatText" class="portal-input mt-2" rows="3" maxlength="2000" placeholder="输入测试消息"></textarea>
              <p class="my-3 text-xs" role="status">{{ chatFeedback }}</p>
              <div class="flex gap-2"><button type="submit" :disabled="chatBusy || !chatText.trim() || !chatModel.trim()" class="omnihive-btn-primary px-4 py-2 text-sm">{{ chatBusy ? '正在等待模型响应…' : '发送消息 →' }}</button><button type="button" :disabled="chatBusy" class="omnihive-btn-outline px-4 py-2 text-sm" @click="clearChat">清空对话</button></div>
            </form>
          </div>
        </section>

        <section class="pub-section">
          <div class="omnihive-card hive-dots flex flex-wrap items-center justify-between gap-5 p-8">
            <div><div class="pub-eyebrow mb-2">One key. Every model.</div><h2 class="text-2xl font-bold">一把 API Key，多种模型。无需订阅、无需多帐号。</h2></div>
            <div class="flex flex-wrap gap-2"><router-link :to="isAuthenticated ? '/keys' : '/register'" class="omnihive-btn-primary px-4 py-2.5 text-sm">获取 API Key</router-link><button class="omnihive-btn-outline px-4 py-2.5 text-sm" @click="navigate('/model-plaza')">浏览模型 ⧉</button></div>
          </div>
        </section>
      </div>
    </main>

    <!-- ===== 7. Complete Footer ===== -->
    <footer class="relative z-10 border-t px-4 py-10 sm:px-6" style="border-color: var(--border, #e9e9ee)">
      <div class="mx-auto max-w-[1200px]">
        <div class="mb-8 grid grid-cols-2 gap-8 text-sm md:grid-cols-5">
          <div class="col-span-2 md:col-span-1"><div class="mb-3 flex items-center gap-2"><span class="text-2xl" style="color:var(--brand, #f59e0b)">⬡</span><b>{{ siteName }}</b></div><p class="text-xs leading-relaxed opacity-55">中立的 AI 网关平台。<br />把算力交给蜂巢，把时间留给自己。</p></div>
          <div v-for="column in footerColumns" :key="column.title"><h3 class="mb-3 font-semibold">{{ column.title }}</h3><div class="space-y-2"><button v-for="[label, action] in column.items" :key="label" class="block text-left opacity-60 hover:opacity-100" @click="footerAction(label, action)">{{ label }}</button></div></div>
        </div>
        <div class="flex flex-col items-center justify-between gap-4 sm:flex-row">
          <div class="flex items-center gap-2.5">
            <svg width="22" height="22" viewBox="0 0 32 32" fill="none" aria-hidden="true">
              <path d="M16 2 27.7 8.8v14.4L16 30 4.3 23.2V8.8Z" fill="var(--brand, #f59e0b)" />
              <path d="M16 8.5 22.5 12.3v7.4L16 23.5l-6.5-3.8v-7.4Z" fill="var(--card, #ffffff)" />
            </svg>
            <span class="text-sm font-bold">{{ siteName }}</span>
            <span class="text-xs opacity-60">
              &copy; {{ currentYear }} {{ siteName }}. {{ t('home.footer.allRightsReserved') }}
            </span>
          </div>
          <div class="flex flex-wrap items-center gap-5 text-sm">
            <router-link to="/legal/privacy" class="text-xs opacity-65">隐私政策</router-link>
            <router-link to="/legal/terms" class="text-xs opacity-65">服务条款</router-link>
            <router-link
              v-if="showModelPlazaEntry"
              to="/model-plaza"
              class="opacity-65 transition-opacity hover:opacity-100"
            >
              {{ t('nav.modelPlaza') }}
            </router-link>
            <router-link
              to="/key-usage"
              class="opacity-65 transition-opacity hover:opacity-100"
            >
              用量查询
            </router-link>
            <a
              v-if="docUrl"
              :href="docUrl"
              target="_blank"
              rel="noopener noreferrer"
              class="opacity-65 transition-opacity hover:opacity-100"
            >
              {{ t('home.docs') }}
            </a>
            <a
              :href="githubUrl"
              target="_blank"
              rel="noopener noreferrer"
              class="opacity-65 transition-opacity hover:opacity-100"
            >
              GitHub
            </a>
          </div>
        </div>
        <div class="omnihive-card hive-dots relative mt-10 overflow-hidden p-10 text-center">
          <svg width="42" height="42" viewBox="0 0 32 32" fill="none" class="floaty mx-auto" aria-hidden="true"><path d="M16 2 27.7 8.8v14.4L16 30 4.3 23.2V8.8Z" fill="var(--brand, #f59e0b)" /><path d="M16 8.5 22.5 12.3v7.4L16 23.5l-6.5-3.8v-7.4Z" fill="var(--card, #fff)" /><circle cx="16" cy="16" r="2.6" fill="var(--brand, #f59e0b)" /></svg>
          <h2 class="mt-4 text-3xl font-bold">把算力交给蜂巢，把时间留给自己。</h2><p class="mt-3 text-sm opacity-60">注册赠送试用额度（示例文案，实际以站点配置为准）。</p>
          <router-link :to="isAuthenticated ? '/keys' : '/register'" class="omnihive-btn-primary mt-5 px-6 py-3 text-sm">免费开始 →</router-link>
        </div>
      </div>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, inject, nextTick, onMounted, onBeforeUnmount, watch } from 'vue'
import { routerKey } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'
import { sanitizeUrl } from '@/utils/url'
import { embeddedSandbox, sanitizeEmbeddedHtml } from '@/utils/embedded-url'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'
import { useThemeDirection } from '@/composables/useThemeDirection'
import { mcpAPI, type RoutingPolicyConfig } from '@/api/mcp'
import { normalizeApiError } from '@/utils/apiError'
import { publicRoot } from '@/components/intro/quickstart'

const { t } = useI18n()

const authStore = useAuthStore()
const appStore = useAppStore()
const {
  isDark,
  initThemeDirection,
  setDarkMode,
  openCommandPalette,
  openDrawer,
  closeDrawer,
  toggleDarkMode,
} = useThemeDirection()
const router = inject(routerKey, null)
const publicNav = ref<HTMLElement | null>(null)
const activeNavMenu = ref('')
const mobileMenuOpen = ref(false)
const NAV_MENUS = [
  { key: 'models', title: '模型', items: [['模型广场', '/model-plaza'], ['文本模型', '/model-plaza?modality=text'], ['图像模型', '/model-plaza?modality=image'], ['嵌入与重排模型', '/model-plaza?modality=embedding']] },
  { key: 'rankings', title: '排行榜', items: [['模型热度榜', 'rank:models'], ['渠道可用性榜', 'rank:channels'], ['成本榜', 'rank:cost']] },
  { key: 'org', title: '组织', items: [['默认工作区', '/workspaces'], ['成员与权限', '/workspaces'], ['账单与用量', '/usage']] },
  { key: 'docs', title: '文档', items: [['快速开始', '/help#quick'], ['API 参考', '/help#api'], ['SDK 与示例', '/help#clients']] },
  { key: 'chat', title: '对话', items: [['打开对话', 'chat'], ['使用 API Key 接入', '/keys']] },
  { key: 'pricing', title: '定价', items: [['充值 / 订阅', '/purchase'], ['模型定价', '/model-plaza'], ['企业方案', '/purchase']] },
]
const drawerFooter = '<button class="btn btn-outline" data-drawer-action="close">关闭</button>'
function escapeHtml(value: string) {
  return value.replace(/[&<>"']/g, character => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[character]!)
}
function drawerLink(label: string, path: string) {
  return `<button class="block w-full rounded-md border p-3 text-left text-sm hover:bg-secondary" style="border-color:var(--border)" data-drawer-nav="${escapeHtml(path)}">${escapeHtml(label)} →</button>`
}
function navigate(path: string) {
  closeDrawer()
  if (router) void router.push(path)
  else window.location.assign(path)
}
function closeNavMenu(restoreFocus = false) {
  const previous = activeNavMenu.value
  activeNavMenu.value = ''
  if (mobileMenuOpen.value) {
    mobileMenuOpen.value = false
    if (restoreFocus) document.getElementById('public-menu-toggle')?.focus()
  }
  if (restoreFocus) document.getElementById(`nav-${previous}`)?.focus()
}
function dismissNav(event: MouseEvent) {
  if (event.target instanceof Node && !publicNav.value?.contains(event.target)) closeNavMenu()
}
function openRanking(kind: string) {
  rankingTab.value = kind === 'channels' || kind === 'cost' ? kind : 'models'
  document.getElementById('rankings')?.scrollIntoView?.({ behavior: 'smooth', block: 'start' })
}
function navAction(action: string, _label: string) {
  closeNavMenu(true)
  if (action.startsWith('/')) return navigate(action)
  if (action.startsWith('rank:')) return openRanking(action.slice(5))
  if (action === 'chat') {
    chatOpen.value = true
    void nextTick(() => {
      chatPanel.value?.scrollIntoView?.({ behavior: 'smooth', block: 'start' })
      chatInput.value?.focus({ preventScroll: true })
    })
    return
  }
  if (action === 'org') {
    navigate('/workspaces')
    return
  }
  if (action === 'sdk') {
    navigate('/help#clients')
    return
  }
  if (action === 'enterprise') navigate('/purchase')
}
function openMethodology() {
  openDrawer('路由策略 · 对比说明', '<div class="space-y-4 text-sm leading-relaxed"><p>选择智能均衡、最低成本、最低延迟或最高吞吐，将偏好保存到账户路由策略。</p><p>实际成本与请求量、模型单价、缓存、失败重试和渠道可用情况有关，预计节省不作为费用承诺。</p><p>可在工作空间策略中心配置数据处理区域、零数据留存与预算提醒。</p></div>', drawerFooter)
}
const chatOpen = ref(false)
const chatPanel = ref<HTMLElement | null>(null)
const chatInput = ref<HTMLTextAreaElement | null>(null)
const chatText = ref('')
const chatModel = ref('gpt-4o')
const chatBusy = ref(false)
const chatFeedback = ref('')
type ChatPayload = Parameters<typeof mcpAPI.sendPlaygroundChat>[0]
interface ChatMessage {
  id: number
  role: string
  text: string
  failed: boolean
  payload?: ChatPayload
  routedGroup?: string
  latencyMs?: number
}
const chatMessages = ref<ChatMessage[]>([])
let chatSequence = 0
let chatRequest = 0
async function requestChatReply(message: ChatMessage) {
  if (chatBusy.value || !message.payload) return
  const request = ++chatRequest
  chatBusy.value = true
  message.failed = false
  message.text = '正在等待网关响应…'
  chatFeedback.value = '正在发送测试消息…'
  try {
    const response = await mcpAPI.sendPlaygroundChat(message.payload)
    if (request !== chatRequest) return
    message.role = response.model || message.payload.model
    message.text = response.reply
    message.routedGroup = response.routed_group || '网关未返回分组'
    message.latencyMs = response.latency_ms
    chatFeedback.value = '模型响应已收到。'
  } catch (error) {
    if (request !== chatRequest) return
    message.role = '网关提示'
    message.failed = true
    message.text = navigator.onLine === false
      ? '网络已断开，消息已保留。恢复连接后可重试。'
      : `请求未完成：${normalizeApiError(error).message || '网关暂时不可用'}。消息已保留，可重试。`
    chatFeedback.value = '暂未收到模型回复。'
  } finally {
    if (request === chatRequest) {
      chatBusy.value = false
      chatInput.value?.focus()
    }
  }
}
async function sendChatMessage() {
  if (chatBusy.value) return
  const text = chatText.value.trim()
  if (!text || text.length > 2000 || !chatModel.value.trim()) {
    chatFeedback.value = !chatModel.value.trim() ? '请输入目标模型 ID。' : !text ? '请输入消息后再发送。' : '消息不能超过 2000 个字符。'
    chatInput.value?.focus()
    return
  }
  chatMessages.value.push({ id: ++chatSequence, role: '你', text, failed: false })
  chatMessages.value.push({
    id: ++chatSequence, role: chatModel.value.trim(), text: '', failed: false,
    payload: { model: chatModel.value.trim(), message: text, strategy: routingStrategy.value }
  })
  chatText.value = ''
  await requestChatReply(chatMessages.value[chatMessages.value.length - 1])
}
async function retryChat(id: number) {
  const message = chatMessages.value.find(item => item.id === id && item.failed)
  if (!message) return
  await requestChatReply(message)
}
function clearChat() {
  chatRequest++
  chatBusy.value = false
  chatMessages.value = []
  chatFeedback.value = '已清空本次对话。'
}

// Site settings - directly from appStore (already initialized from injected config)
const siteName = computed(() => appStore.cachedPublicSettings?.site_name?.trim() || appStore.siteName?.trim() || 'OmniHive')
const siteLogo = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const siteSubtitle = computed(() => {
  const configured = appStore.cachedPublicSettings?.site_subtitle?.trim()
  return !configured || /omniscription|sub2api/i.test(configured) ? '智能算力中转蜂巢 · 一个入口，连接模型与 Agent。' : configured
})
const docUrl = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.doc_url || appStore.docUrl || ''))
const homeContent = computed(() => appStore.cachedPublicSettings?.home_content || '')
const sanitizedHomeContent = computed(() => sanitizeEmbeddedHtml(homeContent.value))
const hasHomeContent = computed(() => homeContent.value.trim().length > 0)
const compactHomeEnabled = computed(() => appStore.cachedPublicSettings?.compact_home_enabled === true)
const modelPlazaEnabled = computed(() => isFeatureFlagEnabled(FeatureFlags.modelPlaza))

// Check if homeContent is a URL (for iframe display)
const isHomeContentUrl = computed(() => {
  const content = homeContent.value.trim()
  return content.startsWith('http://') || content.startsWith('https://')
})

// GitHub URL
const githubUrl = 'https://github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW'

// Auth state
const isAuthenticated = computed(() => authStore.isAuthenticated)
const modelPlazaRequiresAuth = computed(
  () => appStore.cachedPublicSettings?.model_plaza_require_auth === true,
)
const showModelPlazaEntry = computed(
  () => modelPlazaEnabled.value && (isAuthenticated.value || !modelPlazaRequiresAuth.value),
)
const isAdmin = computed(() => authStore.isAdmin)
const dashboardPath = computed(() => isAdmin.value ? '/admin/dashboard' : '/dashboard')
const userInitial = computed(() => {
  const user = authStore.user
  if (!user || !user.email) return ''
  return user.email.charAt(0).toUpperCase()
})

// Current year for footer
const currentYear = computed(() => new Date().getFullYear())

// Theme dropdown & CmdK
function triggerCommandPalette() {
  openCommandPalette()
}

// Toggle theme
function toggleTheme() {
  toggleDarkMode()
}

// Initialize theme
function initTheme() {
  initThemeDirection()
  const savedTheme = localStorage.getItem('theme')
  if (
    savedTheme === 'dark' ||
    (!savedTheme && window.matchMedia('(prefers-color-scheme: dark)').matches)
  ) {
    setDarkMode(true)
  } else {
    setDarkMode(false)
  }
}

// Initialize synchronously so SSR/unit-test initial render has accurate isDark state
initTheme()

// ===== Honeycomb Constellation & Dispatch Simulation =====
const beePaused = ref(false)
const hiveNet = ref<SVGSVGElement | null>(null)
const hiveTip = ref<{ text: string; x: number; y: number } | null>(null)
function toggleBeePause() {
  beePaused.value = !beePaused.value
  if (beePaused.value) hiveNet.value?.pauseAnimations?.()
  else hiveNet.value?.unpauseAnimations?.()
}
function showHiveTip(event: Event, text: string) {
  const node = event.currentTarget as Element
  const bounds = node.getBoundingClientRect()
  const parent = hiveNet.value?.parentElement?.getBoundingClientRect()
  if (!parent) return
  hiveTip.value = { text, x: Math.max(8, Math.min(bounds.left - parent.left, parent.width - 240)), y: bounds.bottom - parent.top + 10 }
}
const hiddenBees = ref<string[]>([])
const activeFlowerIndex = ref<number | null>(null)
const litCells = ref<number[]>([0])
const simRunning = ref(false)
const simFeedback = ref('')
const simLatency = ref(142)
const simLastModel = ref('claude-sonnet-4-5')
const simLastGroup = ref('Anthropic Pro 组')
const simLastTokens = ref(1420)
const simLastCost = ref('$0.0034')
let simTimers: Array<ReturnType<typeof setTimeout>> = []

const flowerNodes = [
  { name: 'Claude', key: 'claude', provider: 'Anthropic', color: '#d97757', x: 1010, y: 105, index: 0 },
  { name: 'GPT', key: 'gpt', provider: 'OpenAI', color: '#10a37f', x: 1130, y: 235, index: 1 },
  { name: 'Gemini', key: 'gemini', provider: 'Google', color: '#4285f4', x: 1090, y: 410, index: 2 },
  { name: 'Grok', key: 'grok', provider: 'xAI', color: '#6b7280', x: 880, y: 490, index: 3 },
  { name: 'DeepSeek', key: 'deepseek', provider: 'DeepSeek', color: '#7c3aed', x: 600, y: 455, index: 4 },
  { name: 'Ollama', key: 'ollama', provider: '自托管', color: '#16a34a', x: 490, y: 190, index: 5 },
]

const hiveCells = [
  { dx: 0, dy: 0, label: '调度中枢' },
  { dx: -30, dy: -17, label: '默认分组' },
  { dx: 0, dy: -34, label: 'Pro 分组' },
  { dx: 30, dy: -17, label: 'Team 分组' },
  { dx: 30, dy: 17, label: '渠道定价' },
  { dx: 0, dy: 34, label: '账号池' },
  { dx: -30, dy: 17, label: '实时计费' },
]

interface BeeSpec {
  name: string; prov: string; c1: string; c2: string
  wing: string; stripes: number; ant: string; deco: string; body: string; tag: string
}
const BEE_SPECS: Record<string, BeeSpec> = {
  claude: { name: 'Claude', prov: 'Anthropic', c1: '#d97757', c2: '#f6c9b4', wing: 'round', stripes: 2, ant: 'curl', deco: 'bow', body: 'chubby', tag: '温柔细腻 · 长上下文与编码' },
  gpt: { name: 'GPT', prov: 'OpenAI', c1: '#10a37f', c2: '#a7ead9', wing: 'long', stripes: 3, ant: 'straight', deco: 'goggles', body: 'slim', tag: '全能主力 · 多模态与工具调用' },
  gemini: { name: 'Gemini', prov: 'Google', c1: '#4285f4', c2: '#bcd4ff', wing: 'double', stripes: 2, ant: 'ball', deco: 'star', body: 'slim', tag: '百万上下文 · 星尘双翼' },
  grok: { name: 'Grok', prov: 'xAI', c1: '#4b5563', c2: '#c7cbd3', wing: 'sharp', stripes: 3, ant: 'straight', deco: 'bolt', body: 'slim', tag: '实时信息 · 尖翼闪电蜂' },
  deepseek: { name: 'DeepSeek', prov: 'DeepSeek', c1: '#7c3aed', c2: '#d9c9ff', wing: 'long', stripes: 2, ant: 'curl', deco: 'cap', body: 'slim', tag: '推理见长 · 高性价比' },
  qwen: { name: 'Qwen', prov: '阿里云', c1: '#ff6a3d', c2: '#ffcbb6', wing: 'round', stripes: 3, ant: 'ball', deco: 'knot', body: 'chubby', tag: '通义千问 · 中文与开源' },
  llama: { name: 'Llama', prov: 'Meta', c1: '#4f6bed', c2: '#c3cdff', wing: 'round', stripes: 2, ant: 'horn', deco: 'fluff', body: 'chubby', tag: '开源基座 · 卷角绒毛蜂' },
  mistral: { name: 'Mistral', prov: 'Mistral AI', c1: '#f59e0b', c2: '#ffe2a8', wing: 'sharp', stripes: 2, ant: 'feather', deco: 'ribbon', body: 'slim', tag: '轻快高效 · 风纹飘带' },
  kimi: { name: 'Kimi', prov: '月之暗面', c1: '#1e3a8a', c2: '#a8bdf5', wing: 'long', stripes: 2, ant: 'curl', deco: 'moon', body: 'slim', tag: '长文本 · 月牙夜行蜂' },
  doubao: { name: '豆包', prov: '字节跳动', c1: '#ec4899', c2: '#fbc9e0', wing: 'round', stripes: 2, ant: 'ball', deco: 'flower', body: 'chubby', tag: '多模态 · 小花圆滚蜂' },
  wenxin: { name: '文心', prov: '百度', c1: '#2563eb', c2: '#b9d0ff', wing: 'double', stripes: 3, ant: 'straight', deco: 'book', body: 'chubby', tag: '知识增强 · 书卷蜂' },
  o3: { name: 'o3 / Codex', prov: 'OpenAI', c1: '#0ea5a4', c2: '#a7f0e8', wing: 'sharp', stripes: 3, ant: 'feather', deco: 'gear', body: 'slim', tag: '深度推理 · 齿轮蜂' },
  ollama: { name: 'Ollama', prov: '自托管', c1: '#16a34a', c2: '#b6edc6', wing: 'round', stripes: 2, ant: 'horn', deco: 'leaf', body: 'chubby', tag: '本地部署 · 绿叶蜂' },
  bedrock: { name: 'Bedrock', prov: 'AWS', c1: '#ff9900', c2: '#ffdda6', wing: 'sharp', stripes: 3, ant: 'straight', deco: 'cube', body: 'slim', tag: '多云托管 · 方块蜂' },
  antigravity: { name: 'Antigravity', prov: 'Antigravity', c1: '#8b9bb4', c2: '#dfe7f2', wing: 'double', stripes: 2, ant: 'ball', deco: 'halo', body: 'slim', tag: 'Claude / Gemini 专用通道' },
  jev: { name: 'Jev', prov: 'TypeSafe', c1: '#ca8a04', c2: '#f5e2a0', wing: 'long', stripes: 2, ant: 'curl', deco: 'diamond', body: 'slim', tag: 'System One 原生协议' },
}
function beeGlyph(key: string) {
  const spec = BEE_SPECS[key] || BEE_SPECS.claude
  const color = spec.c1, pale = spec.c2
  const width = spec.body === 'chubby' ? 15 : 13, height = spec.body === 'chubby' ? 10.5 : 8.5, wingY = -height - 3
  let wings = ''
  if (spec.wing === 'double') wings = `<ellipse cx="-3" cy="${wingY - 1}" rx="12" ry="6" fill="${pale}" opacity=".5" transform="rotate(-18 -3 ${wingY})"/><ellipse cx="5" cy="${wingY - 4}" rx="9" ry="5" fill="${pale}" opacity=".38" transform="rotate(-34 5 ${wingY})"/><ellipse cx="-5" cy="${wingY + 8}" rx="9" ry="4" fill="${pale}" opacity=".3"/><ellipse cx="6" cy="${wingY + 7}" rx="8" ry="4" fill="${pale}" opacity=".26"/>`
  else if (spec.wing === 'long') wings = `<ellipse cx="-2" cy="${wingY}" rx="17" ry="4.8" fill="${pale}" opacity=".55" transform="rotate(-24 -2 ${wingY})"/><ellipse cx="4" cy="${wingY + 3}" rx="13" ry="4" fill="${pale}" opacity=".4" transform="rotate(-38 4 ${wingY})"/>`
  else if (spec.wing === 'sharp') wings = `<path d="M -2 ${wingY + 2} L -19 ${wingY - 9} L -8 ${wingY + 6} Z" fill="${pale}" opacity=".6"/><path d="M 3 ${wingY + 2} L 15 ${wingY - 11} L 9 ${wingY + 6} Z" fill="${pale}" opacity=".45"/>`
  else wings = `<ellipse cx="-4" cy="${wingY}" rx="11" ry="7" fill="${pale}" opacity=".52" transform="rotate(-20 -4 ${wingY})"/><ellipse cx="4" cy="${wingY}" rx="11" ry="7" fill="${pale}" opacity=".52" transform="rotate(20 4 ${wingY})"/>`
  wings += `<path d="M -12 ${wingY - 3} Q -3 ${wingY} 4 ${wingY + 2}" stroke="${color}" stroke-width=".6" fill="none" opacity=".35"/>`
  const stripes = Array.from({ length: spec.stripes }, (_, index) => {
    const position = width - (index + 1) * (width * 1.55 / (spec.stripes + 1))
    return `<path d="M ${position} ${-height + 1.5} Q ${position + 2.4} 0 ${position} ${height - 1.5}" stroke="${pale}" stroke-width="3.1" fill="none" opacity=".95"/>`
  }).join('')
  const head = -width - 2, decorationY = -15
  let antenna = ''
  if (spec.ant === 'curl') antenna = `<path d="M ${head - 3} -7 Q ${head - 9} -19 ${head - 2} -20 Q ${head + 2} -20 ${head} -16" stroke="${color}" stroke-width="1.5" fill="none"/><path d="M ${head + 2} -7 Q ${head + 5} -19 ${head + 10} -18 Q ${head + 13} -18 ${head + 11} -14" stroke="${color}" stroke-width="1.5" fill="none"/>`
  else if (spec.ant === 'horn') antenna = `<path d="M ${head - 3} -7 Q ${head - 10} -15 ${head - 7} -21 Q ${head - 5} -24 ${head - 3} -19" stroke="${color}" stroke-width="2" fill="none"/><path d="M ${head + 3} -7 Q ${head + 9} -15 ${head + 7} -21 Q ${head + 5} -24 ${head + 3} -19" stroke="${color}" stroke-width="2" fill="none"/>`
  else if (spec.ant === 'feather') antenna = `<path d="M ${head - 3} -7 L ${head - 9} -19 M ${head - 9} -19 l -3 -3 M ${head - 9} -19 l 3 -2" stroke="${color}" stroke-width="1.4" fill="none"/><path d="M ${head + 3} -7 L ${head + 9} -19 M ${head + 9} -19 l 3 -3 M ${head + 9} -19 l -3 -2" stroke="${color}" stroke-width="1.4" fill="none"/>`
  else if (spec.ant === 'ball') antenna = `<path d="M ${head - 3} -7 L ${head - 8} -18 M ${head + 3} -7 L ${head + 8} -18" stroke="${color}" stroke-width="1.4" fill="none"/><circle cx="${head - 8}" cy="-19" r="2" fill="${color}"/><circle cx="${head + 8}" cy="-19" r="2" fill="${color}"/>`
  else antenna = `<path d="M ${head - 3} -7 L ${head - 7} -19 M ${head + 3} -7 L ${head + 8} -19" stroke="${color}" stroke-width="1.4" fill="none"/>`
  let decoration = ''
  if (spec.deco === 'bow') decoration = `<path d="M ${head} ${decorationY} l -6 -4 v 8 Z M ${head} ${decorationY} l 6 -4 v 8 Z" fill="${color}" opacity=".9"/><circle cx="${head}" cy="${decorationY}" r="2" fill="${pale}"/>`
  else if (spec.deco === 'goggles') decoration = `<rect x="${head - 7}" y="-5" width="6" height="5" rx="2" fill="none" stroke="${pale}" stroke-width="1.3"/><rect x="${head + 1}" y="-5" width="6" height="5" rx="2" fill="none" stroke="${pale}" stroke-width="1.3"/><path d="M ${head - 1} -2.5 h 2" stroke="${pale}" stroke-width="1.3"/>`
  else if (spec.deco === 'star') decoration = `<path d="M ${head} ${decorationY - 6} l 1.6 3.4 3.7 .5 -2.7 2.6 .7 3.7 -3.3-1.8 -3.3 1.8 .7-3.7 -2.7-2.6 3.7-.5 Z" fill="${pale}"/>`
  else if (spec.deco === 'bolt') decoration = `<path d="M ${head + 1} ${decorationY - 7} l -5 7 h 3 l -2 6 6 -8 h -3 Z" fill="${pale}"/>`
  else if (spec.deco === 'cap') decoration = `<path d="M ${head - 7} ${decorationY} l 7 -5 7 5 -7 3 Z" fill="${color}"/><path d="M ${head + 6} ${decorationY + 1} v 6" stroke="${color}" stroke-width="1.2"/>`
  else if (spec.deco === 'knot') decoration = `<path d="M ${head - 5} ${decorationY} q 5 -8 10 0 q -5 7 -10 0 Z" fill="${color}"/>`
  else if (spec.deco === 'fluff') decoration = [0, 1, 2, 3, 4].map(index => `<circle cx="${head - 6 + index * 3}" cy="${decorationY - 2 - Math.sin(index) * 2}" r="2" fill="${pale}" opacity=".85"/>`).join('')
  else if (spec.deco === 'ribbon') decoration = `<path d="M ${head - 2} ${decorationY} q -10 -3 -14 3 q 8 1 12 -1 M ${head + 2} ${decorationY} q 10 -3 14 3 q -8 1 -12 -1" stroke="${pale}" stroke-width="2" fill="none"/>`
  else if (spec.deco === 'moon') decoration = `<path d="M ${head + 2} ${decorationY - 6} a 6 6 0 1 0 4 9 a 7 7 0 0 1 -4 -9 Z" fill="${pale}"/>`
  else if (spec.deco === 'flower') decoration = [0, 72, 144, 216, 288].map(angle => `<circle cx="${head + Math.cos(angle * Math.PI / 180) * 4}" cy="${decorationY - 2 + Math.sin(angle * Math.PI / 180) * 4}" r="2.2" fill="${pale}"/>`).join('') + `<circle cx="${head}" cy="${decorationY - 2}" r="1.8" fill="var(--brand, #f59e0b)"/>`
  else if (spec.deco === 'book') decoration = `<rect x="${head - 6}" y="${decorationY - 3}" width="12" height="8" rx="1" fill="none" stroke="${pale}" stroke-width="1.4"/><path d="M ${head} ${decorationY - 3} v 8" stroke="${pale}" stroke-width="1.2"/>`
  else if (spec.deco === 'gear') decoration = `<circle cx="${head}" cy="${decorationY - 1}" r="5" fill="none" stroke="${pale}" stroke-width="2" stroke-dasharray="2 2"/><circle cx="${head}" cy="${decorationY - 1}" r="1.8" fill="${pale}"/>`
  else if (spec.deco === 'leaf') decoration = `<path d="M ${head - 1} ${decorationY + 2} q -8 -8 2 -12 q 6 5 -2 12 Z" fill="${pale}"/>`
  else if (spec.deco === 'cube') decoration = `<path d="M ${head} ${decorationY - 6} l 6 3 v 6 l -6 3 -6 -3 v -6 Z" fill="none" stroke="${pale}" stroke-width="1.3"/>`
  else if (spec.deco === 'halo') decoration = `<ellipse cx="${head}" cy="${decorationY}" rx="8" ry="2.6" fill="none" stroke="${pale}" stroke-width="2"/>`
  else if (spec.deco === 'diamond') decoration = `<path d="M ${head} ${decorationY - 7} l 5 5 -5 5 -5 -5 Z" fill="none" stroke="${pale}" stroke-width="1.6"/>`
  const legs = [-7, 0, 7].map(position => `<path d="M ${position} ${height - 1} l -2 5 M ${position} ${height - 1} l 2 5" stroke="${color}" stroke-width="1.1" opacity=".7"/>`).join('')
  return `<g>${wings}${legs}<ellipse cx="0" cy="0" rx="${width}" ry="${height}" fill="${color}"/>${stripes}<path d="M ${width - 1} 0 l 5 -2.5 v 5 Z" fill="${color}" opacity=".9"/><circle cx="${head}" cy="-1" r="7.2" fill="${color}"/><circle cx="${head - 2.6}" cy="-2" r="1.5" fill="#1b1b1f"/><circle cx="${head + 2.4}" cy="-2" r="1.5" fill="#1b1b1f"/><circle cx="${head - 2.2}" cy="-2.5" r=".5" fill="#fff"/><circle cx="${head + 2.8}" cy="-2.5" r=".5" fill="#fff"/><path d="M ${head - 2} 2.4 q 2 2.2 4 0" stroke="#1b1b1f" stroke-width="1" fill="none" opacity=".7"/>${antenna}${decoration}<circle class="nectar" cx="${width + 7}" cy="0" r="3" fill="var(--brand, #f59e0b)"/><circle cx="${width + 7}" cy="0" r="6" fill="var(--brand, #f59e0b)" opacity=".16"/></g>`
}
const beeSearch = ref('')
const filteredBees = computed(() => Object.entries(BEE_SPECS).filter(([key, bee]) => `${key} ${bee.name} ${bee.prov} ${bee.tag}`.toLowerCase().includes(beeSearch.value.trim().toLowerCase())))
const orbitingBees = Object.entries(BEE_SPECS).map(([key, bee], index) => {
  const radiusX = [420, 300, 500, 420, 300, 500, 360, 450][index % 8]
  const radiusY = [180, 120, 220, 180, 120, 220, 160, 200][index % 8]
  const sweep = index % 3 === 0 ? 0 : 1
  return { key, ...bee, dur: 16 + index % 7 * 2.6, path: `M ${820 - radiusX} 280 a ${radiusX} ${radiusY} 0 1 ${sweep} ${2 * radiusX} 0 a ${radiusX} ${radiusY} 0 1 ${sweep} ${-2 * radiusX} 0` }
})
function beeDetail(key: string) {
  const bee = BEE_SPECS[key]
  const wings: Record<string, string> = { round: '圆翅', long: '长翼', double: '双对翼', sharp: '尖翼' }
  openDrawer(`${bee.name} · 蜜蜂档案`, `<div class="space-y-4 text-sm"><div class="flex justify-center rounded-lg border p-6" style="background:color-mix(in srgb,${bee.c1} 8%,var(--card))"><svg width="150" height="110" viewBox="-38 -34 82 66" fill="none" role="img" aria-label="${bee.name} 蜜蜂">${beeGlyph(key)}</svg></div><div class="grid grid-cols-2 gap-3">${[['模型', bee.name], ['提供商', bee.prov], ['形态', wings[bee.wing]], ['花蜜', 'Token · 按量计费']].map(([label, value]) => `<div class="rounded-lg border p-3"><div class="text-xs opacity-60">${label}</div><b>${value}</b></div>`).join('')}</div><p>${bee.tag}。它驮着你的请求花蜜，在蜂巢与「${bee.name} 花丛」之间往返。</p>${drawerLink(`在模型广场查看 ${bee.name}`, `/model-plaza?search=${encodeURIComponent(bee.name)}`)}</div>`, drawerFooter)
}

function toggleBeeFilter(key: string) {
  const idx = hiddenBees.value.indexOf(key)
  if (idx >= 0) {
    hiddenBees.value.splice(idx, 1)
  } else {
    hiddenBees.value.push(key)
  }
}

function selectFlowerNode(flower: (typeof flowerNodes)[number]) {
  activeFlowerIndex.value = flower.index
  simFeedback.value = `🌸 已选中 ${flower.name} 花丛 (${flower.provider}) · 状态健康在线`
  beeDetail(flower.key)
}

function highlightHiveCell(idx: number, label: string) {
  litCells.value = [0, idx]
  simFeedback.value = `⬡ 蜂房单元：${label} · 负载均衡与配额池正常`
  openDrawer(`蜂房 · ${label}`, `<p class="mb-4 text-sm">蜂房承载分组、渠道、账号池与计费。详细运行状态请进入控制台查看。</p>${drawerLink('打开控制台', dashboardPath.value)}`, drawerFooter)
}

const simulationTargets = [
  { name: 'Claude Sonnet 4.5', modelId: 'claude-sonnet-4-5', group: 'Anthropic Pro 组', tokens: 1640, latency: 138, cost: '$0.0036', flower: 0 },
  { name: 'GPT-5.1', modelId: 'gpt-5.1', group: 'OpenAI 旗舰组', tokens: 1920, latency: 152, cost: '$0.0041', flower: 1 },
  { name: 'Gemini 2.5 Pro', modelId: 'gemini-2.5-pro', group: 'Google 专线组', tokens: 2280, latency: 129, cost: '$0.0027', flower: 2 },
  { name: 'DeepSeek V3.1', modelId: 'deepseek-v3.1', group: 'DeepSeek 极速组', tokens: 2540, latency: 116, cost: '$0.0015', flower: 4 },
]
let simCursor = 0

function simulateRequest() {
  if (simRunning.value) return
  simRunning.value = true
  const chosen = simulationTargets[simCursor % simulationTargets.length]
  simCursor += 1

  activeFlowerIndex.value = chosen.flower
  litCells.value = [0, 1, 2, 3, 4, 5, 6]
  simFeedback.value = `🐝 正在由蜂巢中枢调度请求前往 ${chosen.name} (${chosen.group})…`

  const t1 = setTimeout(() => {
    simLatency.value = chosen.latency
    simLastModel.value = chosen.modelId
    simLastGroup.value = chosen.group
    simLastTokens.value = chosen.tokens
    simLastCost.value = chosen.cost
    simFeedback.value = `✓ 调度完成：${chosen.group} 返回 ${chosen.tokens.toLocaleString()} Tokens · 耗时 ${chosen.latency}ms · 实付 ${chosen.cost}`

    const t2 = setTimeout(() => {
      activeFlowerIndex.value = null
      litCells.value = [0]
      simRunning.value = false
    }, 450)
    simTimers.push(t2)
  }, 360)
  simTimers.push(t1)
}

type RankingKind = 'models' | 'channels' | 'cost'
interface RankingRow { name: string; provider: string; metric: string; delta: string; history: number[] }
const rankingTab = ref<RankingKind>('models')
const rankingTabs: { key: RankingKind; label: string }[] = [
  { key: 'models', label: 'Model Token Volume' },
  { key: 'channels', label: 'Provider Uptime & P95' },
  { key: 'cost', label: 'Cost Efficiency' }
]
const referenceRankings: Record<RankingKind, RankingRow[]> = {
  models: [
    { name: 'GPT-4o', provider: 'OpenAI', metric: '1.20B tokens', delta: '+8.2%', history: [40, 46, 42, 53, 52, 68, 74, 81] },
    { name: 'Gemini 2.5 Pro', provider: 'Google', metric: '620M tokens', delta: '+6.4%', history: [28, 33, 40, 38, 46, 52, 59, 62] },
    { name: 'Claude Sonnet 4.5', provider: 'Anthropic', metric: '387M tokens', delta: '+12.1%', history: [12, 18, 16, 24, 29, 27, 34, 39] },
    { name: 'DeepSeek R1', provider: 'DeepSeek', metric: '210M tokens', delta: '+21.0%', history: [7, 8, 11, 10, 14, 16, 18, 21] }
  ],
  channels: [
    { name: 'Anthropic', provider: 'Messages', metric: '99.98%', delta: 'P95 480ms', history: [99.9, 99.96, 99.92, 99.97, 99.98, 99.96, 99.99, 99.98] },
    { name: 'OpenAI', provider: 'Chat / Responses', metric: '99.95%', delta: 'P95 620ms', history: [99.92, 99.9, 99.94, 99.96, 99.92, 99.98, 99.94, 99.95] },
    { name: 'Google', provider: 'Generate Content', metric: '99.91%', delta: 'P95 710ms', history: [99.8, 99.85, 99.82, 99.89, 99.88, 99.93, 99.9, 99.91] }
  ],
  cost: [
    { name: 'DeepSeek R1', provider: '输入 / 1M tokens · 标准参考价', metric: '$0.55', delta: '—', history: [0.8, 0.8, 0.7, 0.7, 0.6, 0.6, 0.55, 0.55] },
    { name: 'Gemini 2.5 Pro', provider: '输入 / 1M tokens · 标准参考价', metric: '$1.25', delta: '—', history: [1.5, 1.5, 1.4, 1.4, 1.25, 1.25, 1.25, 1.25] },
    { name: 'GPT-4o', provider: '输入 / 1M tokens · 标准参考价', metric: '$2.50', delta: '—', history: [3, 3, 2.75, 2.75, 2.5, 2.5, 2.5, 2.5] }
  ]
}
const rankingData = ref<Record<RankingKind, RankingRow[]>>(referenceRankings)
const rankingsLive = ref(false)
const rankingsReference = ref(false)
const rankingsLoading = ref(false)
const rankingUpdated = ref('')
const rankingStale = ref(false)
let rankingsController: AbortController | undefined
let rankingsTimer: ReturnType<typeof setTimeout> | undefined
const rankingStatus = computed(() => rankingsLoading.value ? '正在读取公开统计…' : rankingsLive.value
  ? `${rankingStale.value ? '网络波动，已保留最近快照 · ' : ''}${rankingsReference.value ? '蜂巢周期调度快照' : '网关实时调度统计'} · 同步于 ${rankingUpdated.value}`
  : '蜂巢模型生态快照 · 实际可用性与价格以当前分组目录为准')
function sparkline(values: number[]) {
  const minimum = Math.min(...values)
  const range = Math.max(...values) - minimum
  return values.map((value, index) => `${2 + index * 96 / Math.max(values.length - 1, 1)},${range ? 25 - (value - minimum) / range * 22 : 14}`).join(' ')
}
function rankingRows(value: unknown): RankingRow[] {
  if (!Array.isArray(value)) throw new Error('Invalid ranking rows')
  return value.slice(0, 20).map((row: unknown) => {
    if (!row || typeof row !== 'object') throw new Error('Invalid ranking row')
    const entry = row as Record<string, unknown>
    if (typeof entry.name !== 'string' || typeof entry.metric !== 'string') throw new Error('Invalid ranking metric')
    const history = entry.history ?? entry.sparkline
    return {
      name: entry.name,
      provider: typeof entry.provider === 'string' ? entry.provider : '',
      metric: entry.metric,
      delta: typeof entry.p95_ms === 'number' && Number.isFinite(entry.p95_ms) ? `P95 ${entry.p95_ms}ms` : typeof entry.delta === 'string' ? entry.delta : '',
      history: Array.isArray(history) ? history.filter((point): point is number => typeof point === 'number' && Number.isFinite(point)).slice(-30) : []
    }
  })
}
async function loadRankings() {
  if (rankingsLoading.value) return
  rankingsLoading.value = true
  const controller = new AbortController()
  rankingsController = controller
  rankingsTimer = setTimeout(() => controller.abort(), 8000)
  try {
    const response = await fetch('/api/v1/public/rankings', { signal: controller.signal, credentials: 'omit', headers: { Accept: 'application/json' } })
    if (!response.ok) throw new Error('Rankings unavailable')
    const payload = await response.json()
    if (controller.signal.aborted) return
    const data = payload?.data ?? payload
    rankingData.value = { models: rankingRows(data?.models), channels: rankingRows(data?.channels), cost: rankingRows(data?.cost) }
    rankingsReference.value = response.headers?.get('X-Rankings-Source') === 'curated_weekly_reference'
    rankingsLive.value = true
    rankingStale.value = false
    rankingUpdated.value = new Date().toLocaleTimeString()
  } catch {
    rankingStale.value = rankingsLive.value
  } finally {
    clearTimeout(rankingsTimer)
    rankingsLoading.value = false
  }
}

type SdkTabKey = 'curl' | 'python' | 'cursor' | 'claude' | 'codex' | 'opencode'
const activeSdkTab = ref<SdkTabKey>('opencode')
const sdkCopied = ref(false)
const copyError = ref('')

const sdkTabs: Array<{ key: SdkTabKey; label: string }> = [
  { key: 'opencode', label: 'OpenCode' },
  { key: 'claude', label: 'Claude Code' },
  { key: 'codex', label: 'Codex CLI' },
  { key: 'cursor', label: 'Cursor' },
  { key: 'python', label: 'Python SDK' },
  { key: 'curl', label: 'cURL' },
]
function moveSdkTab(event: KeyboardEvent, index: number) {
  if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
  event.preventDefault()
  const nextIndex = event.key === 'Home' ? 0 : event.key === 'End' ? sdkTabs.length - 1
    : (index + (event.key === 'ArrowRight' ? 1 : -1) + sdkTabs.length) % sdkTabs.length
  activeSdkTab.value = sdkTabs[nextIndex].key
  sdkCopied.value = false
  copyError.value = ''
  void nextTick(() => document.getElementById(`sdk-tab-${activeSdkTab.value}`)?.focus())
}

const sdkSnippets: Record<SdkTabKey, { file: string; code: string }> = {
  opencode: {
    file: 'opencode.json · OpenCode v2',
    code: `{
  "$schema": "https://opencode.ai/config.json",
  "providers": {
    "omnihive": {
      "package": "@opencode/ai/providers/openai-compatible",
      "name": "OmniHive",
      "env": ["OMNIHIVE_API_KEY"],
      "settings": {
        "baseURL": "https://api.omnihive.local/v1"
      },
      "models": { "MODEL_ID": { "name": "MODEL_ID" } }
    }
  },
  "model": "omnihive/MODEL_ID"
}`,
  },
  codex: {
    file: '~/.codex/config.toml',
    code: `model_provider = "omnihive"
model = "MODEL_ID"

[model_providers.omnihive]
name = "OmniHive"
base_url = "https://api.omnihive.local/v1"
env_key = "OMNIHIVE_API_KEY"
wire_api = "responses"`,
  },
  curl: {
    file: 'terminal · bash',
    code: `curl -X POST https://api.omnihive.local/v1/chat/completions \\
  -H "Authorization: Bearer $OMNIHIVE_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "MODEL_ID",
    "messages": [{"role": "user", "content": "Hello OmniHive!"}]
  }'`,
  },
  python: {
    file: 'quickstart.py',
    code: `import os
from openai import OpenAI

client = OpenAI(
    base_url="https://api.omnihive.local/v1",
    api_key=os.environ["OMNIHIVE_API_KEY"],
)

resp = client.chat.completions.create(
    model="MODEL_ID",
    messages=[{"role": "user", "content": "Hello from Python SDK!"}],
)
print(resp.choices[0].message.content)`,
  },
  cursor: {
    file: 'Cursor Settings / Models / API Keys',
    code: `OpenAI API Key       = YOUR_API_KEY
Override OpenAI Base URL = https://api.omnihive.local/v1
Model ID             = MODEL_ID

在 Cursor 的 Models 设置中启用自定义 OpenAI 地址，
输入您的密钥，添加模型广场中的模型 ID 并验证连接。
客户端功能与模型协议支持以当前分组为准。`,
  },
  claude: {
    file: 'terminal / Claude Code',
    code: `export ANTHROPIC_BASE_URL="https://api.omnihive.local"
export ANTHROPIC_AUTH_TOKEN="$OMNIHIVE_API_KEY"
claude --model MODEL_ID`,
  },
}

const sdkRoot = computed(() => {
  const configured = appStore.apiBaseUrl?.trim() || appStore.cachedPublicSettings?.api_base_url?.trim()
  try { return configured ? publicRoot(configured, '') : '' } catch { return '' }
})
const currentSdkSnippet = computed(() => {
  const snippet = sdkSnippets[activeSdkTab.value]
  return { ...snippet, code: snippet.code.split('https://api.omnihive.local').join(sdkRoot.value || 'YOUR_GATEWAY_ROOT') }
})

async function copySdkCode() {
  copyError.value = ''
  try {
    if (!navigator.clipboard?.writeText) throw new Error('Clipboard unavailable')
    await navigator.clipboard.writeText(currentSdkSnippet.value.code)
    sdkCopied.value = true
    const timer = setTimeout(() => {
      sdkCopied.value = false
    }, 1800)
    simTimers.push(timer)
  } catch {
    sdkCopied.value = false
    copyError.value = '复制失败，请手动选择配置并复制。'
  }
}

// ===== Ecosystem & Worldview Data =====
const ecosystemClients = [
  'Claude Code',
  'Codex',
  'Cursor',
  'Cline',
  'Dify',
  'LobeChat',
  'Open WebUI',
  'OpenCode',
]

const worldviewPillars = [
  {
    icon: '🐝',
    title: '蜜蜂 = 请求载体',
    desc: '每一只蜜蜂驮着你的 Prompt 与上下文往返，按健康度自动挑选最佳上游节点。',
  },
  {
    icon: '🍯',
    title: '花蜜 = Token',
    desc: '请求是采出去的花蜜，返回的 Completion 是酿好的蜜，按分组倍率精确到微美分计费。',
  },
  {
    icon: '🌸',
    title: '花丛 = 上游模型',
    desc: 'Claude / GPT / Gemini / DeepSeek 各成花丛，单点限流时毫秒级改派健康花丛。',
  },
  {
    icon: '⬡',
    title: '蜂巢 = OmniHive 本体',
    desc: '六边形蜂房承载分组定价、粘性会话、并发控制与多账号负载均衡。',
  },
]

const painPoints = [
  {
    painTitle: '多平台订阅费用高昂',
    painDesc: 'Claude、OpenAI、Gemini 各订一份，月费叠加且额度经常闲置过期。',
    solution: '按量付费 + 专属分组折扣倍率，用多少付多少',
  },
  {
    painTitle: '多账号密钥散落难管',
    painDesc: '团队多人共用账号频繁触发风控封号，密钥轮换需要逐个改配置。',
    solution: '一把 API Key 统一接入全平台模型，支持子密钥配额',
  },
  {
    painTitle: '单账号并发限流中断',
    painDesc: '高峰期 429 Too Many Requests 频发，编码 Agent 任务半途崩溃。',
    solution: '多账号池智能调度 + 故障毫秒级自动重路由',
  },
  {
    painTitle: '账单黑盒无法归因',
    painDesc: '月底才看到总账单，分不清哪个项目、哪个模型消耗了预算。',
    solution: 'Token 级实时流水、缓存命中明细与公开查用量页面',
  },
]

const comparisonRows = [
  { dim: '计费与结算方式', official: '固定 $20~$200/月，不用完照扣', hive: '按 Token 实付倍率计费 / 灵活订阅套餐' },
  { dim: '模型覆盖广度', official: '单一厂商封闭生态', hive: 'Claude + GPT + Gemini + DeepSeek 一键切换' },
  { dim: '并发与稳定性', official: '单号严格 RPM/TPM 限制，易 429', hive: '百号聚合蜂巢池，异常节点自动隔离切换' },
  { dim: '上下文缓存优化', official: '跨账号无法共享缓存状态', hive: '智能 Sticky Session 粘性路由，缓存命中率拉满' },
  { dim: '成本与用量可见性', official: '粗粒度月度账单', hive: '每次调用输入/输出/缓存读写/耗时实时可查' },
]

// ===== Smart Routing Strategies =====
type RoutingKey = RoutingPolicyConfig['strategy_mode']
const activeRoutingKey = computed(() => homePolicy.value.strategy_mode)
const routingStrategy = activeRoutingKey

const routingStrategies: Array<{
  key: RoutingKey
  label: string
  latency: string
  saving: string
  sla: string
  description: string
  rows: Array<{ model: string; percent: number; color: string }>
}> = [
  {
    key: 'smart',
    label: '智能路由',
    latency: '142ms',
    saving: '+$4,227.07',
    sla: '99.99% 自动容灾',
    description: '综合评估上游账号健康度、Prompt 缓存亲和度与实时费率，兼顾响应速度与 Token 成本。',
    rows: [
      { model: 'Claude Opus 4.5', percent: 5, color: '#d97757' },
      { model: 'GPT-5.1', percent: 10, color: '#10a37f' },
      { model: 'Gemini 3 Pro', percent: 12, color: '#4285f4' },
      { model: 'DeepSeek V3.1', percent: 60, color: '#7c3aed' },
      { model: '其他', percent: 13, color: '#a1a1aa' },
    ],
  },
  {
    key: 'latency',
    label: '最低延迟',
    latency: '94ms',
    saving: '+$1,904.38',
    sla: '99.99% 极速通道',
    description: '优先调度低负载、直连专线且 TTFT（首字时间）最短的上游节点，适合实时结对编程。',
    rows: [
      { model: 'GPT-5.1', percent: 42, color: '#10a37f' },
      { model: 'Gemini 3 Flash', percent: 33, color: '#4285f4' },
      { model: 'Claude Sonnet 4.5', percent: 18, color: '#d97757' },
      { model: '其它', percent: 7, color: '#a1a1aa' },
    ],
  },
  {
    key: 'price',
    label: '最低价',
    latency: '168ms',
    saving: '+$6,810.42',
    sla: '99.95% 经济池',
    description: '优先路由至高折扣倍率分组与闲时优惠时段渠道，最大化每美元可购买的有效 Token 数。',
    rows: [
      { model: 'DeepSeek V3.1', percent: 74, color: '#7c3aed' },
      { model: 'Gemini 3 Flash', percent: 16, color: '#4285f4' },
      { model: 'GPT-5.1 Nano', percent: 7, color: '#10a37f' },
      { model: '其它', percent: 3, color: '#a1a1aa' },
    ],
  },
  {
    key: 'throughput',
    label: '最高吞吐',
    latency: '按实时负载',
    saving: '按实际用量',
    sla: '高并发调度',
    description: '优先选择具备可用并发容量的渠道，适合批量任务。',
    rows: [],
  },
]
routingStrategies.sort((left, right) => ['smart', 'price', 'latency', 'throughput'].indexOf(left.key) - ['smart', 'price', 'latency', 'throughput'].indexOf(right.key))

const currentRoutingStrategy = computed(
  () => routingStrategies.find((s) => s.key === activeRoutingKey.value) ?? routingStrategies[0],
)

// ===== Supported Models Ecosystem =====
const activeCategory = ref('text')
const ecosystemCategories = [
  { key: 'text', label: '文本', count: 120 }, { key: 'image', label: '图像', count: 24 },
  { key: 'video', label: '视频', count: 8 }, { key: 'speech', label: '语音', count: 6 },
  { key: 'transcription', label: '转录', count: 5 }, { key: 'embedding', label: '嵌入', count: 9 },
  { key: 'rerank', label: '重排', count: 3 }, { key: 'decision', label: '决策', count: 6 },
  { key: 'audio', label: '音频', count: 2 },
]

const ecosystemModels = [
  { name: 'DeepSeek V3.1 Flash', badge: 'D', context: '128K', tokens: '6.3T', trend: '+12%', color: '#7c3aed' },
  { name: 'Claude Opus 4.5', badge: 'C', context: '200K', tokens: '1.4T', trend: '-2.1%', color: '#d97757' },
  { name: 'GPT-5.1', badge: 'G', context: '400K', tokens: '0.8T', trend: '+2.4%', color: '#10a37f' },
  { name: 'Gemini 3 Pro', badge: 'G', context: '1M', tokens: '1.9T', trend: '+1.6%', color: '#4285f4' },
  { name: 'Kimi K2', badge: 'K', context: '128K', tokens: '0.6T', trend: '-2.3%', color: '#1e3a8a' },
  { name: 'Grok 4.1', badge: 'X', context: '256K', tokens: '0.9T', trend: '+1.1%', color: '#4b5563' },
]
const modalityExamples: Record<string, string[]> = {
  image: ['GPT Image', 'Imagen', 'FLUX', 'Stable Diffusion', '豆包图像', 'Qwen Image'],
  video: ['Sora', 'Veo', 'Kling', 'Runway', 'Wan', 'Seedance'],
  speech: ['GPT TTS', 'Gemini TTS', 'CosyVoice', 'ElevenLabs', 'Azure Speech', '豆包语音'],
  transcription: ['Whisper', 'GPT Transcribe', 'Gemini Audio', 'Paraformer', 'Deepgram'],
  embedding: ['Text Embedding', 'Gemini Embedding', 'BGE', 'Qwen Embedding', 'Jina Embedding', 'Cohere Embed'],
  rerank: ['BGE Reranker', 'Cohere Rerank', 'Jina Reranker'],
  decision: ['o3', 'DeepSeek R1', 'Claude Thinking', 'Gemini Thinking', 'Qwen Reasoner', 'Grok Reasoning'],
  audio: ['GPT Audio', 'Gemini Live'],
}
const filteredEcosystemModels = computed(() => {
  if (activeCategory.value === 'text') return ecosystemModels
  return (modalityExamples[activeCategory.value] || []).map((name, index) => ({
    name, badge: name[0], context: '示例', tokens: '—', trend: '待统计', color: ecosystemModels[index % ecosystemModels.length].color,
  }))
})
const providers = [
  { name: 'Anthropic', color: '#d97757', nodes: 12 }, { name: 'Amazon Bedrock', color: '#ff9900', nodes: 9 },
  { name: 'Google', color: '#4285f4', nodes: 14 }, { name: 'Google Vertex', color: '#34a853', nodes: 10 },
  { name: 'OpenAI', color: '#10a37f', nodes: 16 }, { name: 'Azure', color: '#0078d4', nodes: 8 },
  { name: 'xAI', color: '#4b5563', nodes: 6 }, { name: 'DeepSeek', color: '#7c3aed', nodes: 7 },
]
const usageHeights = [40, 52, 48, 60, 55, 68, 62, 74, 70, 80, 76, 88, 84, 92]
const usageModels = [{ name: 'GPT-5.1', color: '#10a37f' }, { name: 'Claude Opus 4.5', color: '#d97757' }, { name: 'Gemini 3', color: '#4285f4' }, { name: '其它', color: '#7c3aed' }]
const usageShares = [[40, 28, 22, 10], [32, 35, 20, 13], [35, 25, 25, 15]]
const homePolicy = ref<RoutingPolicyConfig>({
  zdr_enabled: true, allow_fallback: true, auto_prompt_cache: true,
  strategy_mode: 'smart', region_preference: 'global', monthly_budget_usd: 3500, alert_threshold_percent: 80, provider_whitelist: [],
})
type PolicySwitchKey = 'zdr_enabled' | 'allow_fallback' | 'auto_prompt_cache'
const policyLabels: Array<{ key: PolicySwitchKey; label: string }> = [
  { key: 'zdr_enabled', label: '零数据留存 (ZDR)' },
  { key: 'allow_fallback', label: '允许回退渠道' },
  { key: 'auto_prompt_cache', label: '自动提示词缓存' },
]
const policies = computed(() => policyLabels.map(policy => ({ ...policy, enabled: homePolicy.value[policy.key] })))
const policyRegions: Array<{ value: RoutingPolicyConfig['region_preference']; label: string }> = [
  { value: 'global', label: 'Global' }, { value: 'us', label: 'US' },
  { value: 'eu', label: 'EU' }, { value: 'ap', label: 'APAC' },
]
const providerWhitelist = ref('')
const policyBusy = ref(false)
const policySynced = ref(false)
const policyFeedback = ref('')
let policyRequest = 0
function applyHomePolicy(config: RoutingPolicyConfig) {
  homePolicy.value = config
  providerWhitelist.value = config.provider_whitelist.join(', ')
}
async function loadHomePolicy() {
  const request = ++policyRequest
  policySynced.value = false
  policyBusy.value = false
  policyFeedback.value = ''
  applyHomePolicy({
    zdr_enabled: true, allow_fallback: true, auto_prompt_cache: true,
    strategy_mode: 'smart', region_preference: 'global', monthly_budget_usd: 3500, alert_threshold_percent: 80, provider_whitelist: [],
  })
  if (!isAuthenticated.value) return
  policyBusy.value = true
  try {
    const config = await mcpAPI.getRoutingPolicies()
    if (request !== policyRequest) return
    applyHomePolicy(config)
    policySynced.value = true
  } catch (error) {
    if (request === policyRequest) policyFeedback.value = `策略暂时无法读取：${normalizeApiError(error).message || '请稍后重试'}`
  } finally {
    if (request === policyRequest) policyBusy.value = false
  }
}
async function saveHomePolicy(payload: Partial<RoutingPolicyConfig>, feedback: string) {
  if (policyBusy.value) return
  if (!isAuthenticated.value) {
    applyHomePolicy({ ...homePolicy.value, ...payload })
    policySynced.value = false
    policyFeedback.value = '选择已保留在当前页面，登录后可保存并生效。'
    return
  }
  const request = ++policyRequest
  policyBusy.value = true
  policyFeedback.value = '正在保存策略…'
  try {
    const config = await mcpAPI.updateRoutingPolicies(payload)
    if (request !== policyRequest) return
    applyHomePolicy(config)
    policySynced.value = true
    policyFeedback.value = feedback
  } catch (error) {
    if (request !== policyRequest) return
    providerWhitelist.value = homePolicy.value.provider_whitelist.join(', ')
    policyFeedback.value = `策略未更新：${normalizeApiError(error).message || '请稍后重试'}`
  } finally {
    if (request === policyRequest) policyBusy.value = false
  }
}
function togglePolicy(policy: { key: PolicySwitchKey; label: string; enabled: boolean }) {
  void saveHomePolicy({ [policy.key]: !policy.enabled }, `${policy.label}已${policy.enabled ? '关闭' : '开启'}，路由策略已更新。`)
}
function saveProviderWhitelist() {
  const provider_whitelist = [...new Set(providerWhitelist.value.split(/[,，]/).map(value => value.trim()).filter(Boolean))]
  void saveHomePolicy({ provider_whitelist }, '供应商白名单已更新。')
}
watch(() => [isAuthenticated.value, authStore.user?.id], () => {
  clearChat()
  chatText.value = ''
  void loadHomePolicy()
}, { immediate: true })
const complianceItems = [
  ['操作审计日志', '已支持', '#16a34a'], ['分组定价与模型白名单', '已支持', '#16a34a'],
  ['并发与速率限制', '已支持', '#16a34a'], ['零数据留存', '已支持', '#16a34a'],
  ['强制区域内路由', '已支持', '#16a34a'], ['SOC 2 / GDPR 认证', '企业合规支持', '#16a34a'],
]
const showcaseProjects = [
  ['Claude Code', '在终端里把整个仓库交给蜂巢调度', '#1a1a1f'], ['Cline', '开源 VS Code 编码 Agent', '#111827'],
  ['HERMES AGENT', '自主多步任务编排', '#0f0f0f'], ['LobeChat', '一个界面切换多种模型', '#1e293b'],
  ['无限画布', '在画布里连续生成与迭代图像', '#252018'], ['Dify', '把蜂巢接进企业工作流', '#0c1a2b'],
]
function openShowcase(project: string[]) {
  openDrawer(`${project[0]} · 接入案例`, `<p class="mb-3 text-sm">${project[1]}</p><p class="mb-4 text-sm opacity-65">接入示意：创建密钥，将客户端 Base URL 配置为本站网关地址，再选择可用模型。实际模型和协议支持以部署配置为准。</p>${drawerLink('查看接入文档', '/help')}`, drawerFooter)
}
const footerColumns = [
  { title: '产品', items: [['对话', 'chat'], ['模型', '/model-plaza'], ['排行榜', 'rank:models'], ['定价', '/purchase'], ['企业版', 'enterprise']] },
  { title: '资源', items: [['文档', '/help'], ['API 参考', '/help#api'], ['SDK', 'sdk'], ['状态页', '/monitor'], ['博客', 'planned']] },
  { title: '公司', items: [['关于', 'about'], ['客户', '#built-on'], ['招贤纳士', 'planned'], ['服务条款', '/legal/terms'], ['隐私', '/legal/privacy']] },
  { title: '开发者', items: [['GitHub', 'github'], ['更新日志', 'changelog'], ['开发者平台', '/dashboard'], ['API 状态', '/monitor'], ['Discord', 'planned']] },
]
function footerAction(label: string, action: string) {
  if (action === 'github' || action === 'changelog') {
    window.open(action === 'github' ? githubUrl : `${githubUrl}/releases`, '_blank', 'noopener,noreferrer')
    return
  }
  if (action.startsWith('#')) {
    document.querySelector(action)?.scrollIntoView({ behavior: 'smooth' })
    return
  }
  if (action === 'planned' || action === 'about') {
    openDrawer(label, `<p class="text-sm">${action === 'about' ? `${escapeHtml(siteName.value)} · 中立的 AI 网关平台，把算力交给蜂巢，把时间留给自己。` : '此入口为规划内容，站点尚未配置对应地址。'}</p>`, drawerFooter)
    return
  }
  navAction(action, label)
}

const faqItems = [
  {
    q: '支持哪些模型与协议？',
    a: '全面支持 Claude、GPT、Gemini、Grok、DeepSeek 等主流模型，原生兼容 OpenAI Chat/Responses、Anthropic Messages 与 Google Gemini 协议，模型广场可实时查看各分组折后单价。',
  },
  {
    q: '如何计费？什么是分组倍率？',
    a: '平台采用 Token 级精确追踪：实付费用 = 官方基准单价 × 分组倍率（或您的专属倍率）。支持 Prompt 缓存读写独立计价、长上下文阶梯计价及分时优惠折扣。',
  },
  {
    q: '上游账号限流或故障时会影响我的业务请求吗？',
    a: '不会。OmniHive 蜂巢调度中枢实时监测所有上游账号的健康度、RPM/TPM 水位与首字延迟，单节点异常时毫秒级无感重路由至健康节点。',
  },
  {
    q: '可以直接接入 Claude Code、Codex 或 Cursor 吗？',
    a: '完全可以。只需将环境变量中的 Base URL 指向本站网关地址并填入您的 API Key，无需修改任何现有工作流代码。',
  },
]

onMounted(() => {
  void loadRankings()
  initTheme()
  document.addEventListener('click', dismissNav)
  if (window.matchMedia('(prefers-reduced-motion: reduce)').matches && !beePaused.value) toggleBeePause()

  // Check auth state
  authStore.checkAuth()

  // Ensure public settings are loaded (will use cache if already loaded from injected config)
  if (!appStore.publicSettingsLoaded) {
    appStore.fetchPublicSettings()
  }
})

onBeforeUnmount(() => {
  rankingsController?.abort()
  clearTimeout(rankingsTimer)
  policyRequest++
  chatRequest++
  document.removeEventListener('click', dismissNav)
  simTimers.forEach((timer) => clearTimeout(timer))
  simTimers = []
})
</script>

<style scoped>
.compact-emblem { box-shadow: 0 0 64px color-mix(in srgb, var(--brand) 12%, transparent); }
.compact-portal :is(a, button):focus-visible { outline: 2px solid var(--brand); outline-offset: 3px; }
.compact-portal header a:last-child { color: var(--primary-foreground); }
.omnihive-portal :where(button, a, input, textarea, select, summary, [tabindex]):focus-visible {
  outline: 2px solid var(--brand, #f59e0b);
  outline-offset: 4px;
}
.omnihive-portal section { scroll-margin-top: 110px; }
.omnihive-portal .grid > * { min-width: 0; }
.omnihive-portal .terminal-header { flex-wrap: wrap; gap: 8px; }
.omnihive-portal .code-response { overflow-wrap: anywhere; }
.portal-input {
  width: 100%;
  border: 1px solid var(--border, #e9e9ee);
  border-radius: 8px;
  padding: 10px 12px;
  background: var(--card, #fff);
  color: inherit;
  font-size: 13px;
}
select.portal-input { width: auto; }
.bee-card, .showcase-card { transition: transform .2s, border-color .2s, box-shadow .2s; }
.bee-card:hover, .showcase-card:hover {
  transform: translateY(-3px);
  border-color: var(--brand, #f59e0b);
  box-shadow: 0 8px 24px #0000000a;
}
.hive-cell { cursor: pointer; transition: filter .2s; }
.hive-cell:hover, .hive-cell.lit { filter: drop-shadow(0 0 5px var(--brand, #f59e0b)); }
.pain-card { min-height: 180px; }
.pain-fix { display: none; }
.pain-card:hover, .pain-card:focus { border-color: var(--brand, #f59e0b); }
.pain-card:hover .pain-desc, .pain-card:focus .pain-desc { display: none; }
.pain-card:hover .pain-fix, .pain-card:focus .pain-fix { display: block; }
.prov-cell {
  width: 14px;
  height: 14px;
  border-radius: 3px;
  border: 1px solid var(--border, #e9e9ee);
  background: var(--surface-2, #f4f4f5);
  transition: transform .25s;
}
.prov-cell.lit, .prov-cell:hover { background: var(--brand, #f59e0b); border-color: var(--brand-text, #92610a); }
.prov-cell:hover { transform: scale(1.25); }
.portal-meter { height: 6px; border-radius: 20px; background: var(--surface-2, #f4f4f5); overflow: hidden; }
.portal-meter > span { display: block; height: 100%; border-radius: inherit; transition: width .3s; }
.policy-switch { width: 36px; height: 21px; border-radius: 20px; padding: 3px; background: var(--muted, #d4d4d8); transition: background .2s; }
.policy-switch > span { display: block; width: 15px; height: 15px; border-radius: 50%; background: white; transition: transform .2s; }
.policy-switch.on { background: var(--brand, #f59e0b); }
.policy-switch.on > span { transform: translateX(15px); }
.showcase-card { min-height: 112px; }
.mobile-bee { animation: mobileFlight 6s ease-in-out infinite; }
.usage-bar { transform-origin: bottom; animation: growBar .6s both; }
.bee-pause *, .bee-pause :deep(*) { animation-play-state: paused !important; }
@keyframes mobileFlight { 50% { transform: translate(12px, -5px); } }
@keyframes growBar { from { transform: scaleY(.1); } to { transform: scaleY(1); } }
@media (max-width: 1279px) {
  .public-menus { order: 3; width: 100%; justify-content: space-between; }
  .public-menus > div:nth-last-child(-n+3) .omnihive-dropdown { left: auto; right: 0; }
}
@media (max-width: 639px) {
  #pubNav nav > div:first-child { flex: 1 1 100%; }
  #pubNav nav > div:last-child { width: 100%; justify-content: flex-end; }
  .public-menus { gap: 0; }
  .public-menus button { padding-inline: 6px; }
  .pub-section { padding-block: 32px; }
  .hero-copy { padding-top: 36px; }
}
@media (prefers-reduced-motion: reduce) {
  .omnihive-portal *, .omnihive-portal :deep(*) { animation: none !important; transition: none !important; }
}
/* OmniHive Portal Scoped Theme Tokens & Fallbacks */
.omnihive-portal {
  background-color: var(--background, #fafafa);
  color: var(--foreground, #131316);
  font-family: var(--font-sans, 'Inter', 'PingFang SC', 'Microsoft YaHei', system-ui, sans-serif);
}

:global(.dark) .omnihive-portal {
  background-color: var(--background, #0c0c0e);
  color: var(--foreground, #ececf0);
}

.omnihive-glass-bar {
  background: var(--glass, rgba(250, 250, 250, 0.84));
  border-color: var(--border, #e9e9ee);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
}

:global(.dark) .omnihive-glass-bar {
  background: var(--glass, rgba(19, 19, 22, 0.84));
  border-color: var(--border, #232329);
}

.omnihive-hive-pill {
  border-radius: 9999px;
  padding: 1px 6px;
  font-family: var(--font-mono, ui-monospace, monospace);
  font-size: 10px;
  font-weight: 600;
  background: color-mix(in srgb, var(--brand, #f59e0b) 16%, transparent);
  color: var(--brand-text, #92610a);
}

:global(.dark) .omnihive-hive-pill {
  color: var(--brand, #fbbf24);
}

.omnihive-cmdk-btn {
  align-items: center;
  gap: 8px;
  border-radius: 8px;
  border: 1px solid var(--border, #e9e9ee);
  background: var(--surface-2, #f4f4f5);
  color: var(--muted-foreground, #6f6f78);
  padding: 5px 10px;
  transition: border-color 0.18s ease, color 0.18s ease;
}

.omnihive-cmdk-btn:hover {
  border-color: var(--brand, #f59e0b);
  color: var(--foreground, #131316);
}

.omnihive-kbd {
  font-family: var(--font-mono, ui-monospace, monospace);
  font-size: 10px;
  padding: 1px 5px;
  border-radius: 4px;
  border: 1px solid var(--border, #e9e9ee);
  background: var(--card, #ffffff);
}

.omnihive-nav-link {
  opacity: 0.72;
  transition: opacity 0.18s ease, color 0.18s ease;
}

.omnihive-nav-link:hover {
  opacity: 1;
  color: var(--brand-text, #92610a);
}

.omnihive-action-pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  border-radius: 8px;
  border: 1px solid var(--border, #e9e9ee);
  background: var(--card, #ffffff);
  padding: 6px 11px;
  font-size: 12.5px;
  font-weight: 500;
  transition: all 0.18s ease;
}

.omnihive-action-pill:hover {
  border-color: color-mix(in srgb, var(--brand, #f59e0b) 45%, var(--border, #e9e9ee));
  background: var(--secondary, #f4f4f5);
}

.omnihive-icon-btn {
  display: inline-flex;
  height: 34px;
  width: 34px;
  align-items: center;
  justify-content: center;
  border-radius: 8px;
  border: 1px solid var(--border, #e9e9ee);
  background: var(--card, #ffffff);
  color: var(--foreground, #131316);
  transition: all 0.18s ease;
}

.omnihive-icon-btn:hover {
  border-color: color-mix(in srgb, var(--brand, #f59e0b) 45%, var(--border, #e9e9ee));
  background: var(--secondary, #f4f4f5);
}

.omnihive-btn-primary {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  border-radius: 9px;
  background: var(--primary, #16161a);
  color: var(--primary-foreground, #ffffff);
  transition: transform 0.18s ease, filter 0.18s ease;
}

.omnihive-btn-primary:hover {
  filter: brightness(1.08);
  transform: translateY(-1px);
}

.omnihive-btn-outline {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  border-radius: 9px;
  border: 1px solid var(--border, #e9e9ee);
  background: var(--card, #ffffff);
  color: var(--foreground, #131316);
  transition: all 0.18s ease;
}

.omnihive-btn-outline:hover:not(:disabled) {
  border-color: color-mix(in srgb, var(--brand, #f59e0b) 45%, var(--border, #e9e9ee));
  background: var(--secondary, #f4f4f5);
  transform: translateY(-1px);
}

.omnihive-dropdown {
  position: absolute;
  z-index: 50;
  border-radius: 12px;
  border: 1px solid var(--border, #e9e9ee);
  background: var(--popover, #ffffff);
  box-shadow: 0 16px 36px rgba(0, 0, 0, 0.14);
}

.omnihive-dropdown-item {
  display: flex;
  width: 100%;
  align-items: center;
  gap: 8px;
  border-radius: 8px;
  padding: 7px 10px;
  transition: background 0.15s ease;
}

.omnihive-dropdown-item:hover,
.omnihive-dropdown-item.is-active {
  background: var(--secondary, #f4f4f5);
}

.omnihive-hero-section {
  background: linear-gradient(180deg, var(--card, #ffffff), var(--background, #fafafa));
  border-color: var(--border, #e9e9ee);
}

.hero-grid {
  position: relative;
}

#hiveNet {
  position: absolute;
  top: 0;
  bottom: 0;
  left: 44%;
  right: 0;
  width: 56%;
  height: 100%;
}

@media (max-width: 1023px) {
  #hiveNet {
    display: none;
  }
}

.omnihive-card {
  border-radius: var(--radius-md, 12px);
  border: 1px solid var(--border, #e9e9ee);
  background: var(--card, #ffffff);
  color: var(--card-foreground, #131316);
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.03);
}

.omnihive-tag-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  border-radius: 9999px;
  border: 1px solid var(--border, #e9e9ee);
  background: var(--card, #ffffff);
  padding: 4px 11px;
  font-size: 12px;
  font-weight: 500;
}

.brand-gradient {
  background: linear-gradient(100deg, var(--brand, #f59e0b), #ea580c 55%, var(--foreground, #131316));
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
}

.brand-hairline {
  height: 2px;
  width: 36px;
  border-radius: 2px;
  background: var(--brand, #f59e0b);
}

.hive-dots {
  background-image: radial-gradient(circle, color-mix(in srgb, var(--brand, #f59e0b) 28%, transparent) 1px, transparent 1px);
  background-size: 18px 18px;
}

.pub-section {
  padding: 52px 0;
  border-top: 1px solid var(--border, #e9e9ee);
}

.pub-eyebrow {
  font-size: 11.5px;
  font-weight: 600;
  letter-spacing: 0.16em;
  text-transform: uppercase;
  color: var(--muted-foreground, #6f6f78);
}

.pulse-dot {
  width: 7.5px;
  height: 7.5px;
  border-radius: 9999px;
  display: inline-block;
  flex: none;
}

.pulse-ok {
  background: var(--ok, #16a34a);
  color: var(--ok, #16a34a);
  animation: breath 2s cubic-bezier(0.4, 0, 0.6, 1) infinite;
}

@keyframes breath {
  0% {
    box-shadow: 0 0 0 0 color-mix(in srgb, currentColor 65%, transparent);
  }
  65% {
    box-shadow: 0 0 0 7px transparent;
  }
  100% {
    box-shadow: 0 0 0 0 transparent;
  }
}

.segmented {
  display: inline-flex;
  align-items: center;
  background: var(--secondary, #f4f4f5);
  border: 1px solid var(--border, #e9e9ee);
  border-radius: 10px;
  padding: 3px;
  gap: 2px;
}

.segmented button {
  padding: 6px 13px;
  font-size: 12.5px;
  font-weight: 500;
  border-radius: 7px;
  color: var(--muted-foreground, #6f6f78);
  transition: all 0.16s ease;
}

.segmented button.on {
  background: var(--card, #ffffff);
  color: var(--foreground, #131316);
  font-weight: 600;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.08);
}

.legend-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 10px;
  border-radius: 9999px;
  border: 1px solid var(--border, #e9e9ee);
  font-size: 11px;
  background: var(--card, #ffffff);
  cursor: pointer;
  transition: all 0.18s ease;
}

.legend-chip.off {
  opacity: 0.4;
}

.legend-chip i {
  width: 8px;
  height: 8px;
  border-radius: 9999px;
  display: inline-block;
}

@keyframes dashFlow {
  to {
    stroke-dashoffset: -100;
  }
}

.dash-flow {
  animation: dashFlow 8s linear infinite;
}

@keyframes floaty {
  0%,
  100% {
    transform: translateY(0);
  }
  50% {
    transform: translateY(-6px);
  }
}

.floaty {
  animation: floaty 5s ease-in-out infinite;
}

@keyframes beeBob {
  0%,
  100% {
    transform: translateY(0) rotate(-6deg);
  }
  50% {
    transform: translateY(-4px) rotate(6deg);
  }
}

.bee-body {
  animation: beeBob 1.6s ease-in-out infinite;
  transform-origin: center;
}

.flower.is-sim-target {
  filter: drop-shadow(0 0 14px rgba(245, 158, 11, 0.65));
}

.bee-pause .dash-flow,
.bee-pause .floaty,
.bee-pause .bee-body,
.bee-pause .pulse-ok {
  animation-play-state: paused !important;
}

/* Terminal Container */
.terminal-container {
  position: relative;
  display: block;
}

/* Terminal Window */
.terminal-window {
  width: 100%;
  background: var(--card);
  color: var(--foreground);
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: none;
  overflow: hidden;
  transition: transform 0.3s ease;
}

.terminal-window:hover {
  border-color: var(--brand);
}

/* Terminal Header */
.terminal-header {
  display: flex;
  align-items: center;
  padding: 12px 16px;
  background: var(--surface-2);
  border-bottom: 1px solid var(--border);
}

.terminal-buttons {
  display: flex;
  gap: 8px;
}

.terminal-buttons span {
  width: 7px;
  height: 7px;
  border-radius: 50%;
}

.btn-close {
  background: #ef4444;
}
.btn-minimize {
  background: #eab308;
}
.btn-maximize {
  background: #22c55e;
}

.terminal-title {
  flex: 1;
  text-align: center;
  font-size: 12px;
  font-family: ui-monospace, monospace;
  color: #64748b;
  margin-right: 52px;
}

/* Terminal Body */
.terminal-body {
  padding: 20px 24px;
  font-family: ui-monospace, 'Fira Code', monospace;
  font-size: 14px;
  line-height: 2;
}

.code-line {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  opacity: 0;
  animation: line-appear 0.5s ease forwards;
}

.line-1 {
  animation-delay: 0.3s;
}
.line-2 {
  animation-delay: 1s;
}
.line-3 {
  animation-delay: 1.8s;
}
.line-4 {
  animation-delay: 2.5s;
}

@keyframes line-appear {
  from {
    opacity: 0;
    transform: translateY(5px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.code-prompt {
  color: #22c55e;
  font-weight: bold;
}
.code-cmd {
  color: #38bdf8;
}
.code-flag {
  color: #a78bfa;
}
.code-url {
  color: #14b8a6;
}
.code-comment {
  color: #64748b;
  font-style: italic;
}
.code-success {
  color: #22c55e;
  background: rgba(34, 197, 94, 0.15);
  padding: 2px 8px;
  border-radius: 4px;
  font-weight: 600;
}
.code-response {
  color: #fbbf24;
}

/* Blinking Cursor */
.cursor {
  display: inline-block;
  width: 8px;
  height: 16px;
  background: #22c55e;
  animation: blink 1s step-end infinite;
}

@keyframes blink {
  0%,
  50% {
    opacity: 1;
  }
  51%,
  100% {
    opacity: 0;
  }
}

/* Dark mode adjustments */
:deep(.dark) .terminal-window {
  box-shadow: none;
}
.terminal-window pre { color: var(--foreground); }
.terminal-header > div > span { color: var(--muted-foreground); }
.terminal-header button { background: var(--card); color: var(--foreground); border-color: var(--border); }
</style>
