/* @ts-self-types="./boytacean.d.ts" */

export class BessState {
    static __wrap(ptr) {
        const obj = Object.create(BessState.prototype);
        obj.__wbg_ptr = ptr;
        BessStateFinalization.register(obj, obj.__wbg_ptr, obj);
        return obj;
    }
    __destroy_into_raw() {
        const ptr = this.__wbg_ptr;
        this.__wbg_ptr = 0;
        BessStateFinalization.unregister(this);
        return ptr;
    }
    free() {
        const ptr = this.__destroy_into_raw();
        wasm.__wbg_bessstate_free(ptr, 0);
    }
    /**
     * @returns {string}
     */
    agent_wa() {
        let deferred2_0;
        let deferred2_1;
        try {
            const ret = wasm.bessstate_agent_wa(this.__wbg_ptr);
            var ptr1 = ret[0];
            var len1 = ret[1];
            if (ret[3]) {
                ptr1 = 0; len1 = 0;
                throw takeFromExternrefTable0(ret[2]);
            }
            deferred2_0 = ptr1;
            deferred2_1 = len1;
            return getStringFromWasm0(ptr1, len1);
        } finally {
            wasm.__wbindgen_free(deferred2_0, deferred2_1, 1);
        }
    }
    /**
     * @returns {boolean}
     */
    has_image_wa() {
        const ret = wasm.bessstate_has_image_wa(this.__wbg_ptr);
        return ret !== 0;
    }
    /**
     * @returns {Uint8Array}
     */
    image_eager_wa() {
        const ret = wasm.bessstate_image_eager_wa(this.__wbg_ptr);
        if (ret[3]) {
            throw takeFromExternrefTable0(ret[2]);
        }
        var v1 = getArrayU8FromWasm0(ret[0], ret[1]).slice();
        wasm.__wbindgen_free(ret[0], ret[1] * 1, 1);
        return v1;
    }
    /**
     * @returns {string}
     */
    model_wa() {
        let deferred2_0;
        let deferred2_1;
        try {
            const ret = wasm.bessstate_model_wa(this.__wbg_ptr);
            var ptr1 = ret[0];
            var len1 = ret[1];
            if (ret[3]) {
                ptr1 = 0; len1 = 0;
                throw takeFromExternrefTable0(ret[2]);
            }
            deferred2_0 = ptr1;
            deferred2_1 = len1;
            return getStringFromWasm0(ptr1, len1);
        } finally {
            wasm.__wbindgen_free(deferred2_0, deferred2_1, 1);
        }
    }
    /**
     * @returns {bigint}
     */
    timestamp_wa() {
        const ret = wasm.bessstate_timestamp_wa(this.__wbg_ptr);
        if (ret[2]) {
            throw takeFromExternrefTable0(ret[1]);
        }
        return BigInt.asUintN(64, ret[0]);
    }
    /**
     * @returns {string}
     */
    title_wa() {
        let deferred2_0;
        let deferred2_1;
        try {
            const ret = wasm.bessstate_title_wa(this.__wbg_ptr);
            var ptr1 = ret[0];
            var len1 = ret[1];
            if (ret[3]) {
                ptr1 = 0; len1 = 0;
                throw takeFromExternrefTable0(ret[2]);
            }
            deferred2_0 = ptr1;
            deferred2_1 = len1;
            return getStringFromWasm0(ptr1, len1);
        } finally {
            wasm.__wbindgen_free(deferred2_0, deferred2_1, 1);
        }
    }
}
if (Symbol.dispose) BessState.prototype[Symbol.dispose] = BessState.prototype.free;

/**
 * @enum {0 | 1 | 2 | 3 | 4 | 5 | 6 | 7}
 */
export const BootRom = Object.freeze({
    Dmg: 0, "0": "Dmg",
    Sgb: 1, "1": "Sgb",
    DmgBootix: 2, "2": "DmgBootix",
    MgbBootix: 3, "3": "MgbBootix",
    Cgb: 4, "4": "Cgb",
    CgbBoytacean: 5, "5": "CgbBoytacean",
    Other: 6, "6": "Other",
    None: 7, "7": "None",
});

export class BosState {
    static __wrap(ptr) {
        const obj = Object.create(BosState.prototype);
        obj.__wbg_ptr = ptr;
        BosStateFinalization.register(obj, obj.__wbg_ptr, obj);
        return obj;
    }
    __destroy_into_raw() {
        const ptr = this.__wbg_ptr;
        this.__wbg_ptr = 0;
        BosStateFinalization.unregister(this);
        return ptr;
    }
    free() {
        const ptr = this.__destroy_into_raw();
        wasm.__wbg_bosstate_free(ptr, 0);
    }
    /**
     * @returns {string}
     */
    agent_wa() {
        let deferred2_0;
        let deferred2_1;
        try {
            const ret = wasm.bosstate_agent_wa(this.__wbg_ptr);
            var ptr1 = ret[0];
            var len1 = ret[1];
            if (ret[3]) {
                ptr1 = 0; len1 = 0;
                throw takeFromExternrefTable0(ret[2]);
            }
            deferred2_0 = ptr1;
            deferred2_1 = len1;
            return getStringFromWasm0(ptr1, len1);
        } finally {
            wasm.__wbindgen_free(deferred2_0, deferred2_1, 1);
        }
    }
    /**
     * @returns {boolean}
     */
    has_image_wa() {
        const ret = wasm.bosstate_has_image_wa(this.__wbg_ptr);
        return ret !== 0;
    }
    /**
     * @returns {Uint8Array}
     */
    image_eager_wa() {
        const ret = wasm.bosstate_image_eager_wa(this.__wbg_ptr);
        if (ret[3]) {
            throw takeFromExternrefTable0(ret[2]);
        }
        var v1 = getArrayU8FromWasm0(ret[0], ret[1]).slice();
        wasm.__wbindgen_free(ret[0], ret[1] * 1, 1);
        return v1;
    }
    /**
     * @returns {string}
     */
    model_wa() {
        let deferred2_0;
        let deferred2_1;
        try {
            const ret = wasm.bosstate_model_wa(this.__wbg_ptr);
            var ptr1 = ret[0];
            var len1 = ret[1];
            if (ret[3]) {
                ptr1 = 0; len1 = 0;
                throw takeFromExternrefTable0(ret[2]);
            }
            deferred2_0 = ptr1;
            deferred2_1 = len1;
            return getStringFromWasm0(ptr1, len1);
        } finally {
            wasm.__wbindgen_free(deferred2_0, deferred2_1, 1);
        }
    }
    /**
     * @returns {bigint}
     */
    timestamp_wa() {
        const ret = wasm.bosstate_timestamp_wa(this.__wbg_ptr);
        if (ret[2]) {
            throw takeFromExternrefTable0(ret[1]);
        }
        return BigInt.asUintN(64, ret[0]);
    }
    /**
     * @returns {string}
     */
    title_wa() {
        let deferred2_0;
        let deferred2_1;
        try {
            const ret = wasm.bosstate_title_wa(this.__wbg_ptr);
            var ptr1 = ret[0];
            var len1 = ret[1];
            if (ret[3]) {
                ptr1 = 0; len1 = 0;
                throw takeFromExternrefTable0(ret[2]);
            }
            deferred2_0 = ptr1;
            deferred2_1 = len1;
            return getStringFromWasm0(ptr1, len1);
        } finally {
            wasm.__wbindgen_free(deferred2_0, deferred2_1, 1);
        }
    }
}
if (Symbol.dispose) BosState.prototype[Symbol.dispose] = BosState.prototype.free;

export class BoscState {
    static __wrap(ptr) {
        const obj = Object.create(BoscState.prototype);
        obj.__wbg_ptr = ptr;
        BoscStateFinalization.register(obj, obj.__wbg_ptr, obj);
        return obj;
    }
    __destroy_into_raw() {
        const ptr = this.__wbg_ptr;
        this.__wbg_ptr = 0;
        BoscStateFinalization.unregister(this);
        return ptr;
    }
    free() {
        const ptr = this.__destroy_into_raw();
        wasm.__wbg_boscstate_free(ptr, 0);
    }
}
if (Symbol.dispose) BoscState.prototype[Symbol.dispose] = BoscState.prototype.free;

/**
 * Structure that defines the ROM and ROM contents
 * of a Game Boy cartridge.
 *
 * Should correctly address the specifics of all the
 * major MBCs (Memory Bank Controllers).
 */
export class Cartridge {
    static __wrap(ptr) {
        const obj = Object.create(Cartridge.prototype);
        obj.__wbg_ptr = ptr;
        CartridgeFinalization.register(obj, obj.__wbg_ptr, obj);
        return obj;
    }
    __destroy_into_raw() {
        const ptr = this.__wbg_ptr;
        this.__wbg_ptr = 0;
        CartridgeFinalization.unregister(this);
        return ptr;
    }
    free() {
        const ptr = this.__destroy_into_raw();
        wasm.__wbg_cartridge_free(ptr, 0);
    }
    /**
     * @returns {CgbMode}
     */
    cgb_flag() {
        const ret = wasm.cartridge_cgb_flag(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {number}
     */
    checksum() {
        const ret = wasm.cartridge_checksum(this.__wbg_ptr);
        return ret;
    }
    clear_ram_data() {
        wasm.cartridge_clear_ram_data(this.__wbg_ptr);
    }
    /**
     * @param {number} column_length
     * @returns {string}
     */
    description(column_length) {
        let deferred1_0;
        let deferred1_1;
        try {
            const ret = wasm.cartridge_description(this.__wbg_ptr, column_length);
            deferred1_0 = ret[0];
            deferred1_1 = ret[1];
            return getStringFromWasm0(ret[0], ret[1]);
        } finally {
            wasm.__wbindgen_free(deferred1_0, deferred1_1, 1);
        }
    }
    /**
     * @returns {GameBoyMode}
     */
    gb_mode() {
        const ret = wasm.cartridge_gb_mode(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {boolean}
     */
    has_battery() {
        const ret = wasm.cartridge_has_battery(this.__wbg_ptr);
        return ret !== 0;
    }
    /**
     * A cartridge is considered legacy (DMG only) if it does
     * not have a CGB flag bit (bit 7 of 0x0143) set.
     *
     * These are the monochromatic only cartridges built
     * for the original DMG Game Boy.
     * @returns {boolean}
     */
    is_legacy() {
        const ret = wasm.cartridge_is_legacy(this.__wbg_ptr);
        return ret !== 0;
    }
    /**
     * @returns {Licensee}
     */
    licensee() {
        const ret = wasm.cartridge_licensee(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {Uint8Array}
     */
    ram_data_eager() {
        const ret = wasm.cartridge_ram_data_eager(this.__wbg_ptr);
        var v1 = getArrayU8FromWasm0(ret[0], ret[1]).slice();
        wasm.__wbindgen_free(ret[0], ret[1] * 1, 1);
        return v1;
    }
    /**
     * @returns {RamSize}
     */
    ram_size() {
        const ret = wasm.cartridge_ram_size(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {string}
     */
    ram_size_s() {
        let deferred1_0;
        let deferred1_1;
        try {
            const ret = wasm.cartridge_ram_size_s(this.__wbg_ptr);
            deferred1_0 = ret[0];
            deferred1_1 = ret[1];
            return getStringFromWasm0(ret[0], ret[1]);
        } finally {
            wasm.__wbindgen_free(deferred1_0, deferred1_1, 1);
        }
    }
    /**
     * @returns {Region}
     */
    region() {
        const ret = wasm.cartridge_region(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {Uint8Array}
     */
    rom_data_eager() {
        const ret = wasm.cartridge_rom_data_eager(this.__wbg_ptr);
        var v1 = getArrayU8FromWasm0(ret[0], ret[1]).slice();
        wasm.__wbindgen_free(ret[0], ret[1] * 1, 1);
        return v1;
    }
    /**
     * @returns {RomSize}
     */
    rom_size() {
        const ret = wasm.cartridge_rom_size(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {string}
     */
    rom_size_s() {
        let deferred1_0;
        let deferred1_1;
        try {
            const ret = wasm.cartridge_rom_size_s(this.__wbg_ptr);
            deferred1_0 = ret[0];
            deferred1_1 = ret[1];
            return getStringFromWasm0(ret[0], ret[1]);
        } finally {
            wasm.__wbindgen_free(deferred1_0, deferred1_1, 1);
        }
    }
    /**
     * @returns {RomType}
     */
    rom_type() {
        const ret = wasm.cartridge_rom_type(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {string}
     */
    rom_type_s() {
        let deferred1_0;
        let deferred1_1;
        try {
            const ret = wasm.cartridge_rom_type_s(this.__wbg_ptr);
            deferred1_0 = ret[0];
            deferred1_1 = ret[1];
            return getStringFromWasm0(ret[0], ret[1]);
        } finally {
            wasm.__wbindgen_free(deferred1_0, deferred1_1, 1);
        }
    }
    /**
     * @param {Uint8Array} data
     */
    set_ram_data(data) {
        const ptr0 = passArray8ToWasm0(data, wasm.__wbindgen_malloc);
        const len0 = WASM_VECTOR_LEN;
        wasm.cartridge_set_ram_data(this.__wbg_ptr, ptr0, len0);
    }
    /**
     * @returns {SgbMode}
     */
    sgb_flag() {
        const ret = wasm.cartridge_sgb_flag(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {string}
     */
    title() {
        let deferred1_0;
        let deferred1_1;
        try {
            const ret = wasm.cartridge_title(this.__wbg_ptr);
            deferred1_0 = ret[0];
            deferred1_1 = ret[1];
            return getStringFromWasm0(ret[0], ret[1]);
        } finally {
            wasm.__wbindgen_free(deferred1_0, deferred1_1, 1);
        }
    }
    /**
     * @returns {boolean}
     */
    valid_checksum() {
        const ret = wasm.cartridge_valid_checksum(this.__wbg_ptr);
        return ret !== 0;
    }
}
if (Symbol.dispose) Cartridge.prototype[Symbol.dispose] = Cartridge.prototype.free;

/**
 * @enum {0 | 128 | 192}
 */
export const CgbMode = Object.freeze({
    NoCgb: 0, "0": "NoCgb",
    CgbCompatible: 128, "128": "CgbCompatible",
    CgbOnly: 192, "192": "CgbOnly",
});

export class ClockFrame {
    static __wrap(ptr) {
        const obj = Object.create(ClockFrame.prototype);
        obj.__wbg_ptr = ptr;
        ClockFrameFinalization.register(obj, obj.__wbg_ptr, obj);
        return obj;
    }
    __destroy_into_raw() {
        const ptr = this.__wbg_ptr;
        this.__wbg_ptr = 0;
        ClockFrameFinalization.unregister(this);
        return ptr;
    }
    free() {
        const ptr = this.__destroy_into_raw();
        wasm.__wbg_clockframe_free(ptr, 0);
    }
    /**
     * @returns {Uint8Array | undefined}
     */
    frame_buffer_eager() {
        const ret = wasm.clockframe_frame_buffer_eager(this.__wbg_ptr);
        let v1;
        if (ret[0] !== 0) {
            v1 = getArrayU8FromWasm0(ret[0], ret[1]).slice();
            wasm.__wbindgen_free(ret[0], ret[1] * 1, 1);
        }
        return v1;
    }
    /**
     * @returns {bigint}
     */
    get cycles() {
        const ret = wasm.__wbg_get_clockframe_cycles(this.__wbg_ptr);
        return BigInt.asUintN(64, ret);
    }
    /**
     * @returns {number}
     */
    get frames() {
        const ret = wasm.__wbg_get_clockframe_frames(this.__wbg_ptr);
        return ret;
    }
    /**
     * @param {bigint} arg0
     */
    set cycles(arg0) {
        wasm.__wbg_set_clockframe_cycles(this.__wbg_ptr, arg0);
    }
    /**
     * @param {number} arg0
     */
    set frames(arg0) {
        wasm.__wbg_set_clockframe_frames(this.__wbg_ptr, arg0);
    }
}
if (Symbol.dispose) ClockFrame.prototype[Symbol.dispose] = ClockFrame.prototype.free;

export class FromGbOptions {
    static __wrap(ptr) {
        const obj = Object.create(FromGbOptions.prototype);
        obj.__wbg_ptr = ptr;
        FromGbOptionsFinalization.register(obj, obj.__wbg_ptr, obj);
        return obj;
    }
    __destroy_into_raw() {
        const ptr = this.__wbg_ptr;
        this.__wbg_ptr = 0;
        FromGbOptionsFinalization.unregister(this);
        return ptr;
    }
    free() {
        const ptr = this.__destroy_into_raw();
        wasm.__wbg_fromgboptions_free(ptr, 0);
    }
    /**
     * @param {boolean} thumbnail
     * @param {StateFormat | null} [state_format]
     * @param {string | null} [agent]
     * @param {string | null} [agent_version]
     * @returns {FromGbOptions}
     */
    static new(thumbnail, state_format, agent, agent_version) {
        var ptr0 = isLikeNone(agent) ? 0 : passStringToWasm0(agent, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc);
        var len0 = WASM_VECTOR_LEN;
        var ptr1 = isLikeNone(agent_version) ? 0 : passStringToWasm0(agent_version, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc);
        var len1 = WASM_VECTOR_LEN;
        const ret = wasm.fromgboptions_new(thumbnail, isLikeNone(state_format) ? 0 : state_format, ptr0, len0, ptr1, len1);
        return FromGbOptions.__wrap(ret);
    }
}
if (Symbol.dispose) FromGbOptions.prototype[Symbol.dispose] = FromGbOptions.prototype.free;

/**
 * Top level structure that abstracts the usage of the
 * Game Boy system under the Boytacean emulator.
 *
 * Should serve as the main entry-point API.
 */
export class GameBoy {
    __destroy_into_raw() {
        const ptr = this.__wbg_ptr;
        this.__wbg_ptr = 0;
        GameBoyFinalization.unregister(this);
        return ptr;
    }
    free() {
        const ptr = this.__destroy_into_raw();
        wasm.__wbg_gameboy_free(ptr, 0);
    }
    /**
     * @param {string} code
     * @returns {boolean}
     */
    add_cheat_code_wa(code) {
        const ptr0 = passStringToWasm0(code, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc);
        const len0 = WASM_VECTOR_LEN;
        const ret = wasm.gameboy_add_cheat_code_wa(this.__wbg_ptr, ptr0, len0);
        if (ret[2]) {
            throw takeFromExternrefTable0(ret[1]);
        }
        return ret[0] !== 0;
    }
    /**
     * @param {number} cycles
     */
    apu_clock(cycles) {
        wasm.gameboy_apu_clock(this.__wbg_ptr, cycles);
    }
    /**
     * @returns {boolean}
     */
    apu_enabled() {
        const ret = wasm.gameboy_apu_enabled(this.__wbg_ptr);
        return ret !== 0;
    }
    attach_null_serial() {
        wasm.gameboy_attach_null_serial(this.__wbg_ptr);
    }
    attach_printer_serial() {
        wasm.gameboy_attach_printer_serial(this.__wbg_ptr);
    }
    attach_stdout_serial() {
        wasm.gameboy_attach_stdout_serial(this.__wbg_ptr);
    }
    /**
     * @returns {Uint16Array}
     */
    audio_all_output() {
        const ret = wasm.gameboy_audio_all_output(this.__wbg_ptr);
        var v1 = getArrayU16FromWasm0(ret[0], ret[1]).slice();
        wasm.__wbindgen_free(ret[0], ret[1] * 2, 2);
        return v1;
    }
    /**
     * @param {boolean} clear
     * @returns {Int16Array}
     */
    audio_buffer_eager(clear) {
        const ret = wasm.gameboy_audio_buffer_eager(this.__wbg_ptr, clear);
        var v1 = getArrayI16FromWasm0(ret[0], ret[1]).slice();
        wasm.__wbindgen_free(ret[0], ret[1] * 2, 2);
        return v1;
    }
    /**
     * @returns {boolean}
     */
    audio_ch1_enabled() {
        const ret = wasm.gameboy_audio_ch1_enabled(this.__wbg_ptr);
        return ret !== 0;
    }
    /**
     * @returns {number}
     */
    audio_ch1_output() {
        const ret = wasm.gameboy_audio_ch1_output(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {boolean}
     */
    audio_ch2_enabled() {
        const ret = wasm.gameboy_audio_ch2_enabled(this.__wbg_ptr);
        return ret !== 0;
    }
    /**
     * @returns {number}
     */
    audio_ch2_output() {
        const ret = wasm.gameboy_audio_ch2_output(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {boolean}
     */
    audio_ch3_enabled() {
        const ret = wasm.gameboy_audio_ch3_enabled(this.__wbg_ptr);
        return ret !== 0;
    }
    /**
     * @returns {number}
     */
    audio_ch3_output() {
        const ret = wasm.gameboy_audio_ch3_output(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {boolean}
     */
    audio_ch4_enabled() {
        const ret = wasm.gameboy_audio_ch4_enabled(this.__wbg_ptr);
        return ret !== 0;
    }
    /**
     * @returns {number}
     */
    audio_ch4_output() {
        const ret = wasm.gameboy_audio_ch4_output(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {number}
     */
    audio_channels() {
        const ret = wasm.gameboy_audio_channels(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {number}
     */
    audio_filter_mode_wa() {
        const ret = wasm.gameboy_audio_filter_mode_wa(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {number}
     */
    audio_output() {
        const ret = wasm.gameboy_audio_output(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {number}
     */
    audio_sampling_rate() {
        const ret = wasm.gameboy_audio_sampling_rate(this.__wbg_ptr);
        return ret;
    }
    /**
     * Direct boot method that immediately jumps the machine
     * to the post boot state, this will effectively skip the
     * boot sequence and jump to the cartridge execution.
     */
    boot() {
        wasm.gameboy_boot(this.__wbg_ptr);
    }
    /**
     * @returns {BootRom}
     */
    boot_rom() {
        const ret = wasm.gameboy_boot_rom(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {string}
     */
    boot_rom_s() {
        let deferred1_0;
        let deferred1_1;
        try {
            const ret = wasm.gameboy_boot_rom_s(this.__wbg_ptr);
            deferred1_0 = ret[0];
            deferred1_1 = ret[1];
            return getStringFromWasm0(ret[0], ret[1]);
        } finally {
            wasm.__wbindgen_free(deferred1_0, deferred1_1, 1);
        }
    }
    /**
     * @returns {Cartridge}
     */
    cartridge_eager() {
        const ret = wasm.gameboy_cartridge_eager(this.__wbg_ptr);
        return Cartridge.__wrap(ret);
    }
    /**
     * Advance the clock of the system by one tick, this will
     * usually imply executing one CPU instruction and advancing
     * all the other components of the system by the required
     * amount of cycles.
     *
     * This method takes into account the current speed of the
     * system (single or double) and will execute the required
     * amount of cycles in the other components of the system
     * accordingly.
     *
     * The amount of cycles executed by the CPU is returned.
     * @returns {number}
     */
    clock() {
        const ret = wasm.gameboy_clock(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {number}
     */
    clock_freq() {
        const ret = wasm.gameboy_clock_freq(this.__wbg_ptr);
        return ret >>> 0;
    }
    /**
     * @returns {string}
     */
    clock_freq_s() {
        let deferred1_0;
        let deferred1_1;
        try {
            const ret = wasm.gameboy_clock_freq_s(this.__wbg_ptr);
            deferred1_0 = ret[0];
            deferred1_1 = ret[1];
            return getStringFromWasm0(ret[0], ret[1]);
        } finally {
            wasm.__wbindgen_free(deferred1_0, deferred1_1, 1);
        }
    }
    /**
     * Risky function that will clock the CPU multiple times
     * allowing an undefined number of cycles to be executed
     * in the other Game Boy components.
     *
     * This can cause unwanted behaviour in components like
     * the PPU where only one mode switch operation is expected
     * per each clock call.
     *
     * At the end of this execution major synchronization issues
     * may arise, so use with caution.
     * @param {number} count
     * @returns {number}
     */
    clock_many(count) {
        const ret = wasm.gameboy_clock_many(this.__wbg_ptr, count);
        return ret;
    }
    /**
     * Function equivalent to [`clock()`][GameBoy::clock()] but that allows pre-emptive
     * breaking of the clock cycle loop if the PC (Program Counter)
     * reaches the provided address, making sure that in such a situation
     * the devices are not clocked.
     * @param {number} addr
     * @returns {number}
     */
    clock_step(addr) {
        const ret = wasm.gameboy_clock_step(this.__wbg_ptr, addr);
        return ret;
    }
    /**
     * Equivalent to [`clock()`][GameBoy::clock()] but allows the execution of multiple
     * clock operations in a single call.
     * @param {number} count
     * @returns {bigint}
     */
    clocks(count) {
        const ret = wasm.gameboy_clocks(this.__wbg_ptr, count);
        return BigInt.asUintN(64, ret);
    }
    /**
     * Clocks the emulator until the limit of cycles that has been
     * provided and returns the amount of cycles that have been
     * clocked.
     * @param {number} limit
     * @returns {bigint}
     */
    clocks_cycles(limit) {
        const ret = wasm.gameboy_clocks_cycles(this.__wbg_ptr, limit);
        return BigInt.asUintN(64, ret);
    }
    /**
     * Clocks the emulator until the limit of cycles that has been
     * provided and returns the amount of cycles that have been
     * clocked together with the frame buffer of the PPU.
     *
     * Allows a caller to clock the emulator and at the same time
     * retrieve the frame buffer of the PPU at the proper timing
     * (on V-Blank).
     *
     * This method allows for complex foreign call optimizations
     * by preventing the need to call the emulator clock multiple
     * times to obtain the right frame buffer retrieval timing.
     * @param {number} limit
     * @returns {ClockFrame}
     */
    clocks_frame_buffer(limit) {
        const ret = wasm.gameboy_clocks_frame_buffer(this.__wbg_ptr, limit);
        return ClockFrame.__wrap(ret);
    }
    /**
     * @returns {number}
     */
    cpu_clock() {
        const ret = wasm.gameboy_cpu_clock(this.__wbg_ptr);
        return ret;
    }
    /**
     * @param {number} column_length
     * @returns {string}
     */
    description(column_length) {
        let deferred1_0;
        let deferred1_1;
        try {
            const ret = wasm.gameboy_description(this.__wbg_ptr, column_length);
            deferred1_0 = ret[0];
            deferred1_1 = ret[1];
            return getStringFromWasm0(ret[0], ret[1]);
        } finally {
            wasm.__wbindgen_free(deferred1_0, deferred1_1, 1);
        }
    }
    /**
     * @returns {string}
     */
    description_debug() {
        let deferred1_0;
        let deferred1_1;
        try {
            const ret = wasm.gameboy_description_debug(this.__wbg_ptr);
            deferred1_0 = ret[0];
            deferred1_1 = ret[1];
            return getStringFromWasm0(ret[0], ret[1]);
        } finally {
            wasm.__wbindgen_free(deferred1_0, deferred1_1, 1);
        }
    }
    /**
     * @returns {number}
     */
    display_height() {
        const ret = wasm.gameboy_display_height(this.__wbg_ptr);
        return ret >>> 0;
    }
    /**
     * @returns {number}
     */
    display_width() {
        const ret = wasm.gameboy_display_width(this.__wbg_ptr);
        return ret >>> 0;
    }
    /**
     * @param {number} cycles
     */
    dma_clock(cycles) {
        wasm.gameboy_dma_clock(this.__wbg_ptr, cycles);
    }
    /**
     * @returns {boolean}
     */
    dma_enabled() {
        const ret = wasm.gameboy_dma_enabled(this.__wbg_ptr);
        return ret !== 0;
    }
    /**
     * @returns {Uint8Array}
     */
    frame_buffer_eager() {
        const ret = wasm.gameboy_frame_buffer_eager(this.__wbg_ptr);
        var v1 = getArrayU8FromWasm0(ret[0], ret[1]).slice();
        wasm.__wbindgen_free(ret[0], ret[1] * 1, 1);
        return v1;
    }
    /**
     * @returns {Uint8Array}
     */
    frame_buffer_raw_eager() {
        const ret = wasm.gameboy_frame_buffer_raw_eager(this.__wbg_ptr);
        var v1 = getArrayU8FromWasm0(ret[0], ret[1]).slice();
        wasm.__wbindgen_free(ret[0], ret[1] * 1, 1);
        return v1;
    }
    /**
     * Obtains the tile structure for the tile at the
     * given index, no conversion in the pixel buffer
     * is done so that the color reference is the GB one.
     * @param {number} index
     * @returns {Tile}
     */
    get_tile(index) {
        const ret = wasm.gameboy_get_tile(this.__wbg_ptr, index);
        return Tile.__wrap(ret);
    }
    /**
     * Obtains the pixel buffer for the tile at the
     * provided index, converting the color buffer
     * using the currently loaded (background) palette.
     * @param {number} index
     * @returns {Uint8Array}
     */
    get_tile_buffer(index) {
        const ret = wasm.gameboy_get_tile_buffer(this.__wbg_ptr, index);
        var v1 = getArrayU8FromWasm0(ret[0], ret[1]).slice();
        wasm.__wbindgen_free(ret[0], ret[1] * 1, 1);
        return v1;
    }
    /**
     * @returns {Uint8Array}
     */
    hram_eager() {
        const ret = wasm.gameboy_hram_eager(this.__wbg_ptr);
        var v1 = getArrayU8FromWasm0(ret[0], ret[1]).slice();
        wasm.__wbindgen_free(ret[0], ret[1] * 1, 1);
        return v1;
    }
    /**
     * Updates the emulation mode using the cartridge info
     * for the provided data to obtain the CGB flag value
     * and set the mode accordingly.
     *
     * This can be an expensive operation as it will require
     * cartridge data parsing to obtain the CGB flag.
     * It will also have to clone the data buffer.
     * @param {Uint8Array} data
     */
    infer_mode_wa(data) {
        const ptr0 = passArray8ToWasm0(data, wasm.__wbindgen_malloc);
        const len0 = WASM_VECTOR_LEN;
        const ret = wasm.gameboy_infer_mode_wa(this.__wbg_ptr, ptr0, len0);
        if (ret[1]) {
            throw takeFromExternrefTable0(ret[0]);
        }
    }
    /**
     * @returns {boolean}
     */
    is_cgb() {
        const ret = wasm.gameboy_is_cgb(this.__wbg_ptr);
        return ret !== 0;
    }
    /**
     * @returns {boolean}
     */
    is_dmg() {
        const ret = wasm.gameboy_is_dmg(this.__wbg_ptr);
        return ret !== 0;
    }
    /**
     * @returns {boolean}
     */
    is_sgb() {
        const ret = wasm.gameboy_is_sgb(this.__wbg_ptr);
        return ret !== 0;
    }
    /**
     * @param {PadKey} key
     */
    key_lift(key) {
        wasm.gameboy_key_lift(this.__wbg_ptr, key);
    }
    /**
     * @param {PadKey} key
     */
    key_press(key) {
        wasm.gameboy_key_press(this.__wbg_ptr, key);
    }
    /**
     * Loads the machine directly to after the boot execution state,
     * setting the state of the system accordingly and updating the
     * Program Counter (PC) to the post boot address (0x0100).
     *
     * Should allow the machine to jump to the cartridge (ROM) execution
     * directly, skipping the boot sequence.
     *
     * Currently supports only DMG machines.
     */
    load_boot_state() {
        wasm.gameboy_load_boot_state(this.__wbg_ptr);
    }
    load_callbacks_wa() {
        wasm.gameboy_load_callbacks_wa(this.__wbg_ptr);
    }
    load_logger_wa() {
        wasm.gameboy_load_logger_wa(this.__wbg_ptr);
    }
    load_null_wa() {
        wasm.gameboy_load_null_wa(this.__wbg_ptr);
    }
    load_printer_wa() {
        wasm.gameboy_load_printer_wa(this.__wbg_ptr);
    }
    /**
     * @param {Uint8Array} data
     * @returns {Cartridge}
     */
    load_rom_wa(data) {
        const ptr0 = passArray8ToWasm0(data, wasm.__wbindgen_malloc);
        const len0 = WASM_VECTOR_LEN;
        const ret = wasm.gameboy_load_rom_wa(this.__wbg_ptr, ptr0, len0);
        if (ret[2]) {
            throw takeFromExternrefTable0(ret[1]);
        }
        return Cartridge.__wrap(ret[0]);
    }
    /**
     * Unsafe load strategy that will panic the current system
     * in case there are boot ROM loading issues.
     * @param {boolean} boot
     */
    load_unsafe(boot) {
        wasm.gameboy_load_unsafe(this.__wbg_ptr, boot);
    }
    /**
     * @returns {GameBoyMode}
     */
    mode() {
        const ret = wasm.gameboy_mode(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {number}
     */
    multiplier() {
        const ret = wasm.gameboy_multiplier(this.__wbg_ptr);
        return ret;
    }
    /**
     * @param {GameBoyMode | null} [mode]
     */
    constructor(mode) {
        const ret = wasm.gameboy_new(isLikeNone(mode) ? 0 : mode);
        this.__wbg_ptr = ret;
        GameBoyFinalization.register(this, this.__wbg_ptr, this);
        return this;
    }
    /**
     * Clocks the system until the next frame is reached and returns
     * the amount of cycles that have been clocked.
     *
     * This function is used to clock the system until the next frame
     * is reached and returns the amount of cycles that have been
     * clocked.
     * @returns {number}
     */
    next_frame() {
        const ret = wasm.gameboy_next_frame(this.__wbg_ptr);
        return ret >>> 0;
    }
    /**
     * @param {number} cycles
     */
    ppu_clock(cycles) {
        wasm.gameboy_ppu_clock(this.__wbg_ptr, cycles);
    }
    /**
     * @returns {boolean}
     */
    ppu_enabled() {
        const ret = wasm.gameboy_ppu_enabled(this.__wbg_ptr);
        return ret !== 0;
    }
    /**
     * @returns {number}
     */
    ppu_frame() {
        const ret = wasm.gameboy_ppu_frame(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {number}
     */
    ppu_ly() {
        const ret = wasm.gameboy_ppu_ly(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {PpuMode}
     */
    ppu_mode() {
        const ret = wasm.gameboy_ppu_mode(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {Uint8Array}
     */
    ram_data_eager() {
        const ret = wasm.gameboy_ram_data_eager(this.__wbg_ptr);
        var v1 = getArrayU8FromWasm0(ret[0], ret[1]).slice();
        wasm.__wbindgen_free(ret[0], ret[1] * 1, 1);
        return v1;
    }
    /**
     * @returns {RamSize}
     */
    ram_size() {
        const ret = wasm.gameboy_ram_size(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {Registers}
     */
    registers() {
        const ret = wasm.gameboy_registers(this.__wbg_ptr);
        return Registers.__wrap(ret);
    }
    reload_wa() {
        const ret = wasm.gameboy_reload_wa(this.__wbg_ptr);
        if (ret[1]) {
            throw takeFromExternrefTable0(ret[0]);
        }
    }
    /**
     * Resets the entire state of the Game Boy system,
     * including all its components and the CPU.
     *
     * This method ensures that the [`Ppu`], [`Apu`], [`Timer`],
     * [`Serial`], [`Mmu`], and [`Cpu`] are all reset to their initial states.
     */
    reset() {
        wasm.gameboy_reset(this.__wbg_ptr);
    }
    /**
     * Resets all the cheats currently registered in the system.
     *
     * It clears both Game Genie and GameShark cheats from the
     * respective cheat managers.
     */
    reset_cheats() {
        wasm.gameboy_reset_cheats(this.__wbg_ptr);
    }
    /**
     * @param {number} cycles
     */
    serial_clock(cycles) {
        wasm.gameboy_serial_clock(this.__wbg_ptr, cycles);
    }
    /**
     * @returns {boolean}
     */
    serial_enabled() {
        const ret = wasm.gameboy_serial_enabled(this.__wbg_ptr);
        return ret !== 0;
    }
    /**
     * @param {boolean} value
     */
    set_all_enabled(value) {
        wasm.gameboy_set_all_enabled(this.__wbg_ptr, value);
    }
    /**
     * @param {boolean} value
     */
    set_apu_enabled(value) {
        wasm.gameboy_set_apu_enabled(this.__wbg_ptr, value);
    }
    /**
     * @param {boolean} enabled
     */
    set_audio_ch1_enabled(enabled) {
        wasm.gameboy_set_audio_ch1_enabled(this.__wbg_ptr, enabled);
    }
    /**
     * @param {boolean} enabled
     */
    set_audio_ch2_enabled(enabled) {
        wasm.gameboy_set_audio_ch2_enabled(this.__wbg_ptr, enabled);
    }
    /**
     * @param {boolean} enabled
     */
    set_audio_ch3_enabled(enabled) {
        wasm.gameboy_set_audio_ch3_enabled(this.__wbg_ptr, enabled);
    }
    /**
     * @param {boolean} enabled
     */
    set_audio_ch4_enabled(enabled) {
        wasm.gameboy_set_audio_ch4_enabled(this.__wbg_ptr, enabled);
    }
    /**
     * @param {number} mode
     */
    set_audio_filter_mode_wa(mode) {
        wasm.gameboy_set_audio_filter_mode_wa(this.__wbg_ptr, mode);
    }
    /**
     * @param {BootRom} value
     */
    set_boot_rom(value) {
        wasm.gameboy_set_boot_rom(this.__wbg_ptr, value);
    }
    /**
     * @param {number} value
     */
    set_clock_freq(value) {
        wasm.gameboy_set_clock_freq(this.__wbg_ptr, value);
    }
    /**
     * @param {boolean} value
     */
    set_dma_enabled(value) {
        wasm.gameboy_set_dma_enabled(this.__wbg_ptr, value);
    }
    /**
     * @param {GameBoyMode} value
     */
    set_mode(value) {
        wasm.gameboy_set_mode(this.__wbg_ptr, value);
    }
    /**
     * @param {any[]} value
     */
    set_palette_colors_wa(value) {
        const ptr0 = passArrayJsValueToWasm0(value, wasm.__wbindgen_malloc);
        const len0 = WASM_VECTOR_LEN;
        wasm.gameboy_set_palette_colors_wa(this.__wbg_ptr, ptr0, len0);
    }
    static set_panic_hook_wa() {
        wasm.gameboy_set_panic_hook_wa();
    }
    /**
     * @param {boolean} value
     */
    set_ppu_enabled(value) {
        wasm.gameboy_set_ppu_enabled(this.__wbg_ptr, value);
    }
    /**
     * @param {Uint8Array} ram_data
     */
    set_ram_data(ram_data) {
        const ptr0 = passArray8ToWasm0(ram_data, wasm.__wbindgen_malloc);
        const len0 = WASM_VECTOR_LEN;
        wasm.gameboy_set_ram_data(this.__wbg_ptr, ptr0, len0);
    }
    /**
     * @param {boolean} value
     */
    set_serial_enabled(value) {
        wasm.gameboy_set_serial_enabled(this.__wbg_ptr, value);
    }
    /**
     * @param {boolean} value
     */
    set_timer_enabled(value) {
        wasm.gameboy_set_timer_enabled(this.__wbg_ptr, value);
    }
    /**
     * @returns {GameBoySpeed}
     */
    speed() {
        const ret = wasm.gameboy_speed(this.__wbg_ptr);
        return ret;
    }
    /**
     * Clocks the system until the Program Counter (PC) reaches the
     * provided address and returns the amount of cycles that have been
     * clocked.
     *
     * This advances the emulation step by step, accumulating the number of
     * cycles spent until the CPU's PC matches `addr`. Useful for precise
     * debugging or driving the emulator to a known execution point.
     * @param {number} addr
     * @returns {number}
     */
    step_to(addr) {
        const ret = wasm.gameboy_step_to(this.__wbg_ptr, addr);
        return ret >>> 0;
    }
    /**
     * @param {number} cycles
     */
    timer_clock(cycles) {
        wasm.gameboy_timer_clock(this.__wbg_ptr, cycles);
    }
    /**
     * @returns {boolean}
     */
    timer_enabled() {
        const ret = wasm.gameboy_timer_enabled(this.__wbg_ptr);
        return ret !== 0;
    }
    /**
     * Verifies if the provided data represents a valid Game Boy ROM.
     *
     * It is used to verify if the provided data is a valid ROM
     * before loading it into the Game Boy.
     * Preventing the loading of invalid ROMs into the Game Boy which
     * would cause the emulator to crash at runtime.
     *
     * This approach is not guaranteed to be 100% accurate, but it
     * is a good (enough) way to verify if the provided data is
     * a valid ROM.
     * @param {Uint8Array} data
     * @returns {boolean}
     */
    static verify_rom(data) {
        const ptr0 = passArray8ToWasm0(data, wasm.__wbindgen_malloc);
        const len0 = WASM_VECTOR_LEN;
        const ret = wasm.gameboy_verify_rom(ptr0, len0);
        return ret !== 0;
    }
    /**
     * @returns {Uint8Array}
     */
    vram_eager() {
        const ret = wasm.gameboy_vram_eager(this.__wbg_ptr);
        var v1 = getArrayU8FromWasm0(ret[0], ret[1]).slice();
        wasm.__wbindgen_free(ret[0], ret[1] * 1, 1);
        return v1;
    }
    /**
     * @returns {RamSize}
     */
    vram_size() {
        const ret = wasm.gameboy_vram_size(this.__wbg_ptr);
        return ret;
    }
}
if (Symbol.dispose) GameBoy.prototype[Symbol.dispose] = GameBoy.prototype.free;

export class GameBoyConfig {
    __destroy_into_raw() {
        const ptr = this.__wbg_ptr;
        this.__wbg_ptr = 0;
        GameBoyConfigFinalization.unregister(this);
        return ptr;
    }
    free() {
        const ptr = this.__destroy_into_raw();
        wasm.__wbg_gameboyconfig_free(ptr, 0);
    }
    /**
     * @returns {boolean}
     */
    apu_enabled() {
        const ret = wasm.gameboyconfig_apu_enabled(this.__wbg_ptr);
        return ret !== 0;
    }
    /**
     * @returns {number}
     */
    clock_freq() {
        const ret = wasm.gameboyconfig_clock_freq(this.__wbg_ptr);
        return ret >>> 0;
    }
    /**
     * @returns {boolean}
     */
    dma_enabled() {
        const ret = wasm.gameboyconfig_dma_enabled(this.__wbg_ptr);
        return ret !== 0;
    }
    /**
     * @returns {boolean}
     */
    is_cgb() {
        const ret = wasm.gameboyconfig_is_cgb(this.__wbg_ptr);
        return ret !== 0;
    }
    /**
     * @returns {boolean}
     */
    is_dmg() {
        const ret = wasm.gameboyconfig_is_dmg(this.__wbg_ptr);
        return ret !== 0;
    }
    /**
     * @returns {boolean}
     */
    is_sgb() {
        const ret = wasm.gameboyconfig_is_sgb(this.__wbg_ptr);
        return ret !== 0;
    }
    /**
     * @returns {GameBoyMode}
     */
    mode() {
        const ret = wasm.gameboyconfig_mode(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {boolean}
     */
    ppu_enabled() {
        const ret = wasm.gameboyconfig_ppu_enabled(this.__wbg_ptr);
        return ret !== 0;
    }
    /**
     * @returns {boolean}
     */
    serial_enabled() {
        const ret = wasm.gameboyconfig_serial_enabled(this.__wbg_ptr);
        return ret !== 0;
    }
    /**
     * @param {boolean} value
     */
    set_apu_enabled(value) {
        wasm.gameboyconfig_set_apu_enabled(this.__wbg_ptr, value);
    }
    /**
     * @param {number} value
     */
    set_clock_freq(value) {
        wasm.gameboyconfig_set_clock_freq(this.__wbg_ptr, value);
    }
    /**
     * @param {boolean} value
     */
    set_dma_enabled(value) {
        wasm.gameboyconfig_set_dma_enabled(this.__wbg_ptr, value);
    }
    /**
     * @param {GameBoyMode} value
     */
    set_mode(value) {
        wasm.gameboyconfig_set_mode(this.__wbg_ptr, value);
    }
    /**
     * @param {boolean} value
     */
    set_ppu_enabled(value) {
        wasm.gameboyconfig_set_ppu_enabled(this.__wbg_ptr, value);
    }
    /**
     * @param {boolean} value
     */
    set_serial_enabled(value) {
        wasm.gameboyconfig_set_serial_enabled(this.__wbg_ptr, value);
    }
    /**
     * @param {boolean} value
     */
    set_timer_enabled(value) {
        wasm.gameboyconfig_set_timer_enabled(this.__wbg_ptr, value);
    }
    /**
     * @returns {boolean}
     */
    timer_enabled() {
        const ret = wasm.gameboyconfig_timer_enabled(this.__wbg_ptr);
        return ret !== 0;
    }
}
if (Symbol.dispose) GameBoyConfig.prototype[Symbol.dispose] = GameBoyConfig.prototype.free;

/**
 * @enum {0 | 1 | 3 | 4 | 5 | 6 | 7 | 8 | 100}
 */
export const GameBoyDevice = Object.freeze({
    Cpu: 0, "0": "Cpu",
    Mmu: 1, "1": "Mmu",
    Ppu: 3, "3": "Ppu",
    Apu: 4, "4": "Apu",
    Dma: 5, "5": "Dma",
    Pad: 6, "6": "Pad",
    Timer: 7, "7": "Timer",
    Serial: 8, "8": "Serial",
    Unknown: 100, "100": "Unknown",
});

/**
 * Enumeration that describes the multiple running
 * modes of the Game Boy emulator.
 *
 * DMG = Original Game Boy
 * CGB = Game Boy Color
 * SGB = Super Game Boy
 * @enum {1 | 2 | 3}
 */
export const GameBoyMode = Object.freeze({
    Dmg: 1, "1": "Dmg",
    Cgb: 2, "2": "Cgb",
    Sgb: 3, "3": "Sgb",
});

/**
 * @enum {0 | 1}
 */
export const GameBoySpeed = Object.freeze({
    Normal: 0, "0": "Normal",
    Double: 1, "1": "Double",
});

export class GameGenie {
    __destroy_into_raw() {
        const ptr = this.__wbg_ptr;
        this.__wbg_ptr = 0;
        GameGenieFinalization.unregister(this);
        return ptr;
    }
    free() {
        const ptr = this.__destroy_into_raw();
        wasm.__wbg_gamegenie_free(ptr, 0);
    }
}
if (Symbol.dispose) GameGenie.prototype[Symbol.dispose] = GameGenie.prototype.free;

/**
 * Implementation of the GameShark cheat code system
 * that "patches" RAM entries, making use of the V-Blank
 * time to do that.
 *
 * The codes in the GameShark system are in an hexadecimal
 * ASCII format in the form of "ABCDGHEF" where:
 * AB = RAM bank
 * CD = New data
 * GH = Address LSB
 * EF = Address MSB
 *
 * [Wikipedia - GameShark](https://en.wikipedia.org/wiki/GameShark)
 */
export class GameShark {
    __destroy_into_raw() {
        const ptr = this.__wbg_ptr;
        this.__wbg_ptr = 0;
        GameSharkFinalization.unregister(this);
        return ptr;
    }
    free() {
        const ptr = this.__destroy_into_raw();
        wasm.__wbg_gameshark_free(ptr, 0);
    }
}
if (Symbol.dispose) GameShark.prototype[Symbol.dispose] = GameShark.prototype.free;

export class Info {
    __destroy_into_raw() {
        const ptr = this.__wbg_ptr;
        this.__wbg_ptr = 0;
        InfoFinalization.unregister(this);
        return ptr;
    }
    free() {
        const ptr = this.__destroy_into_raw();
        wasm.__wbg_info_free(ptr, 0);
    }
    /**
     * @returns {string}
     */
    static compilation_date() {
        let deferred1_0;
        let deferred1_1;
        try {
            const ret = wasm.info_compilation_date();
            deferred1_0 = ret[0];
            deferred1_1 = ret[1];
            return getStringFromWasm0(ret[0], ret[1]);
        } finally {
            wasm.__wbindgen_free(deferred1_0, deferred1_1, 1);
        }
    }
    /**
     * @returns {string}
     */
    static compilation_time() {
        let deferred1_0;
        let deferred1_1;
        try {
            const ret = wasm.info_compilation_time();
            deferred1_0 = ret[0];
            deferred1_1 = ret[1];
            return getStringFromWasm0(ret[0], ret[1]);
        } finally {
            wasm.__wbindgen_free(deferred1_0, deferred1_1, 1);
        }
    }
    /**
     * Obtains the name of the compiler that has been
     * used in the compilation of the base Boytacean
     * library. Can be used for diagnostics.
     * @returns {string}
     */
    static compiler() {
        let deferred1_0;
        let deferred1_1;
        try {
            const ret = wasm.info_compiler();
            deferred1_0 = ret[0];
            deferred1_1 = ret[1];
            return getStringFromWasm0(ret[0], ret[1]);
        } finally {
            wasm.__wbindgen_free(deferred1_0, deferred1_1, 1);
        }
    }
    /**
     * @returns {string}
     */
    static compiler_version() {
        let deferred1_0;
        let deferred1_1;
        try {
            const ret = wasm.info_compiler_version();
            deferred1_0 = ret[0];
            deferred1_1 = ret[1];
            return getStringFromWasm0(ret[0], ret[1]);
        } finally {
            wasm.__wbindgen_free(deferred1_0, deferred1_1, 1);
        }
    }
    /**
     * Obtains the name of the emulator.
     * @returns {string}
     */
    static name() {
        let deferred1_0;
        let deferred1_1;
        try {
            const ret = wasm.info_name();
            deferred1_0 = ret[0];
            deferred1_1 = ret[1];
            return getStringFromWasm0(ret[0], ret[1]);
        } finally {
            wasm.__wbindgen_free(deferred1_0, deferred1_1, 1);
        }
    }
    /**
     * Obtains the name of the emulator in lowercase.
     * Useful for file paths and other cases where.
     * @returns {string}
     */
    static name_lower() {
        let deferred1_0;
        let deferred1_1;
        try {
            const ret = wasm.info_name_lower();
            deferred1_0 = ret[0];
            deferred1_1 = ret[1];
            return getStringFromWasm0(ret[0], ret[1]);
        } finally {
            wasm.__wbindgen_free(deferred1_0, deferred1_1, 1);
        }
    }
    /**
     * Obtains the system this emulator is emulating.
     * @returns {string}
     */
    static system() {
        let deferred1_0;
        let deferred1_1;
        try {
            const ret = wasm.info_system();
            deferred1_0 = ret[0];
            deferred1_1 = ret[1];
            return getStringFromWasm0(ret[0], ret[1]);
        } finally {
            wasm.__wbindgen_free(deferred1_0, deferred1_1, 1);
        }
    }
    /**
     * Obtains the version of the emulator.
     * @returns {string}
     */
    static version() {
        let deferred1_0;
        let deferred1_1;
        try {
            const ret = wasm.info_version();
            deferred1_0 = ret[0];
            deferred1_1 = ret[1];
            return getStringFromWasm0(ret[0], ret[1]);
        } finally {
            wasm.__wbindgen_free(deferred1_0, deferred1_1, 1);
        }
    }
    /**
     * @returns {string | undefined}
     */
    static wasm_engine() {
        const ret = wasm.info_wasm_engine();
        let v1;
        if (ret[0] !== 0) {
            v1 = getStringFromWasm0(ret[0], ret[1]).slice();
            wasm.__wbindgen_free(ret[0], ret[1] * 1, 1);
        }
        return v1;
    }
}
if (Symbol.dispose) Info.prototype[Symbol.dispose] = Info.prototype.free;

/**
 * @enum {0 | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 | 9 | 10 | 11 | 12 | 13 | 14 | 15 | 16 | 17 | 18 | 19 | 20 | 21 | 22 | 23 | 24 | 25 | 26 | 27 | 28 | 29 | 30 | 31 | 32 | 33 | 34 | 35 | 36 | 37 | 38 | 39 | 40 | 41 | 42 | 43 | 44 | 45 | 46 | 47 | 48 | 49 | 50 | 51 | 52 | 53 | 54 | 55 | 56 | 57 | 58 | 59 | 60 | 61 | 62 | 63 | 64 | 65 | 66 | 67 | 68 | 69 | 70 | 71 | 72 | 73 | 74 | 75 | 76 | 77 | 78 | 79 | 80 | 81 | 82 | 83 | 84 | 85 | 86 | 87 | 88 | 89 | 90 | 91 | 92 | 93 | 94 | 95 | 96 | 97 | 98 | 99 | 100 | 101 | 102 | 103 | 104 | 105 | 106 | 107 | 108 | 109 | 110 | 111 | 112 | 113 | 114 | 115 | 116 | 117 | 118 | 119 | 120 | 121 | 122 | 123 | 124 | 125 | 126 | 127 | 128 | 129 | 130 | 131 | 132 | 133 | 134 | 135 | 136 | 137 | 138}
 */
export const Licensee = Object.freeze({
    Unknown: 0, "0": "Unknown",
    None: 1, "1": "None",
    Nintendo: 2, "2": "Nintendo",
    Capcom: 3, "3": "Capcom",
    HOTB: 4, "4": "HOTB",
    Jaleco: 5, "5": "Jaleco",
    CoconutsJapan: 6, "6": "CoconutsJapan",
    EliteSystems: 7, "7": "EliteSystems",
    EAElectronicArts: 8, "8": "EAElectronicArts",
    HudsonSoft: 9, "9": "HudsonSoft",
    ITCEntertainment: 10, "10": "ITCEntertainment",
    Yanoman: 11, "11": "Yanoman",
    JapanClary: 12, "12": "JapanClary",
    VirginGamesLtd: 13, "13": "VirginGamesLtd",
    PCMComplete: 14, "14": "PCMComplete",
    SanX: 15, "15": "SanX",
    Kemco: 16, "16": "Kemco",
    SETACorporation: 17, "17": "SETACorporation",
    Infogrames: 18, "18": "Infogrames",
    Bandai: 19, "19": "Bandai",
    NewLicensee: 20, "20": "NewLicensee",
    Konami: 21, "21": "Konami",
    HectorSoft: 22, "22": "HectorSoft",
    Banpresto: 23, "23": "Banpresto",
    EntertainmentI: 24, "24": "EntertainmentI",
    Gremlin: 25, "25": "Gremlin",
    UbiSoft: 26, "26": "UbiSoft",
    Atlus: 27, "27": "Atlus",
    MalibuInteractive: 28, "28": "MalibuInteractive",
    Angel: 29, "29": "Angel",
    SpectrumHoloby: 30, "30": "SpectrumHoloby",
    Irem: 31, "31": "Irem",
    USGold: 32, "32": "USGold",
    Absolute: 33, "33": "Absolute",
    AcclaimEntertainment: 34, "34": "AcclaimEntertainment",
    Activision: 35, "35": "Activision",
    SammyUSACorporation: 36, "36": "SammyUSACorporation",
    GameTek: 37, "37": "GameTek",
    ParkPlace: 38, "38": "ParkPlace",
    LJN: 39, "39": "LJN",
    Matchbox: 40, "40": "Matchbox",
    MiltonBradleyCompany: 41, "41": "MiltonBradleyCompany",
    Mindscape: 42, "42": "Mindscape",
    Romstar: 43, "43": "Romstar",
    NaxatSoft: 44, "44": "NaxatSoft",
    Tradewest: 45, "45": "Tradewest",
    TitusInteractive: 46, "46": "TitusInteractive",
    OceanSoftware: 47, "47": "OceanSoftware",
    EliteSystems2: 48, "48": "EliteSystems2",
    ElectroBrain: 49, "49": "ElectroBrain",
    InterplayEntertainment: 50, "50": "InterplayEntertainment",
    Broderbund: 51, "51": "Broderbund",
    SculpturedSoftware: 52, "52": "SculpturedSoftware",
    TheSalesCurveLimited: 53, "53": "TheSalesCurveLimited",
    THQ: 54, "54": "THQ",
    Accolade: 55, "55": "Accolade",
    TriffixEntertainment: 56, "56": "TriffixEntertainment",
    Microprose: 57, "57": "Microprose",
    MisawaEntertainment: 58, "58": "MisawaEntertainment",
    Lozc: 59, "59": "Lozc",
    TokumaShoten: 60, "60": "TokumaShoten",
    BulletProofSoftware: 61, "61": "BulletProofSoftware",
    VicTokai: 62, "62": "VicTokai",
    Ape: 63, "63": "Ape",
    IMax: 64, "64": "IMax",
    Chunsoft: 65, "65": "Chunsoft",
    VideoSystem: 66, "66": "VideoSystem",
    TsubarayaProductions: 67, "67": "TsubarayaProductions",
    Varie: 68, "68": "Varie",
    YonezawaSpal: 69, "69": "YonezawaSpal",
    Arc: 70, "70": "Arc",
    NihonBussan: 71, "71": "NihonBussan",
    Tecmo: 72, "72": "Tecmo",
    Imagineer: 73, "73": "Imagineer",
    Nova: 74, "74": "Nova",
    HoriElectric: 75, "75": "HoriElectric",
    Kawada: 76, "76": "Kawada",
    Takara: 77, "77": "Takara",
    TechnosJapan: 78, "78": "TechnosJapan",
    ToeiAnimation: 79, "79": "ToeiAnimation",
    Toho: 80, "80": "Toho",
    Namco: 81, "81": "Namco",
    ASCIICorporationOrNexsoft: 82, "82": "ASCIICorporationOrNexsoft",
    SquareEnix: 83, "83": "SquareEnix",
    HALLaboratory: 84, "84": "HALLaboratory",
    SNK: 85, "85": "SNK",
    PonyCanyon: 86, "86": "PonyCanyon",
    CultureBrain: 87, "87": "CultureBrain",
    Sunsoft: 88, "88": "Sunsoft",
    SonyImagesoft: 89, "89": "SonyImagesoft",
    SammyCorporation: 90, "90": "SammyCorporation",
    Taito: 91, "91": "Taito",
    Square: 92, "92": "Square",
    DataEast: 93, "93": "DataEast",
    Tonkinhouse: 94, "94": "Tonkinhouse",
    Koei: 95, "95": "Koei",
    UFL: 96, "96": "UFL",
    Ultra: 97, "97": "Ultra",
    Vap: 98, "98": "Vap",
    UseCorporation: 99, "99": "UseCorporation",
    Meldac: 100, "100": "Meldac",
    PoneyCanyon: 101, "101": "PoneyCanyon",
    Sofel: 102, "102": "Sofel",
    Quest: 103, "103": "Quest",
    SigmaEnterprises: 104, "104": "SigmaEnterprises",
    ASKKodanshaCo: 105, "105": "ASKKodanshaCo",
    CopyaSystem: 106, "106": "CopyaSystem",
    Tomy: 107, "107": "Tomy",
    NCS: 108, "108": "NCS",
    Human: 109, "109": "Human",
    Altron: 110, "110": "Altron",
    TowaChiki: 111, "111": "TowaChiki",
    Yutaka: 112, "112": "Yutaka",
    Epcoh: 113, "113": "Epcoh",
    Athena: 114, "114": "Athena",
    AsmikAceEntertainment: 115, "115": "AsmikAceEntertainment",
    Natsume: 116, "116": "Natsume",
    KingRecords: 117, "117": "KingRecords",
    EpicSonyRecords: 118, "118": "EpicSonyRecords",
    IGS: 119, "119": "IGS",
    AWave: 120, "120": "AWave",
    ExtremeEntertainment: 121, "121": "ExtremeEntertainment",
    NintendoResearchDevelopment1: 122, "122": "NintendoResearchDevelopment1",
    BAI: 123, "123": "BAI",
    KSS: 124, "124": "KSS",
    PlanningOfficeWADA: 125, "125": "PlanningOfficeWADA",
    Viacom: 126, "126": "Viacom",
    HiTechExpressions: 127, "127": "HiTechExpressions",
    Mattel: 128, "128": "Mattel",
    LucasfilmGames: 129, "129": "LucasfilmGames",
    TsukudaOriginal: 130, "130": "TsukudaOriginal",
    ChunsoftCo: 131, "131": "ChunsoftCo",
    OceanSoftwareAcclaimEntertainment: 132, "132": "OceanSoftwareAcclaimEntertainment",
    Kaneko: 133, "133": "Kaneko",
    PackInVideo: 134, "134": "PackInVideo",
    BottomUp: 135, "135": "BottomUp",
    KonamiYuGiOh: 136, "136": "KonamiYuGiOh",
    MTO: 137, "137": "MTO",
    Kodansha: 138, "138": "Kodansha",
});

/**
 * @enum {0 | 1 | 2 | 3 | 4 | 5 | 6 | 7}
 */
export const MbcType = Object.freeze({
    NoMbc: 0, "0": "NoMbc",
    Mbc1: 1, "1": "Mbc1",
    Mbc2: 2, "2": "Mbc2",
    Mbc3: 3, "3": "Mbc3",
    Mbc5: 4, "4": "Mbc5",
    Mbc6: 5, "5": "Mbc6",
    Mbc7: 6, "6": "Mbc7",
    Unknown: 7, "7": "Unknown",
});

export class ObjectData {
    __destroy_into_raw() {
        const ptr = this.__wbg_ptr;
        this.__wbg_ptr = 0;
        ObjectDataFinalization.unregister(this);
        return ptr;
    }
    free() {
        const ptr = this.__destroy_into_raw();
        wasm.__wbg_objectdata_free(ptr, 0);
    }
}
if (Symbol.dispose) ObjectData.prototype[Symbol.dispose] = ObjectData.prototype.free;

/**
 * @enum {0 | 1 | 2 | 3 | 4 | 5 | 6 | 7}
 */
export const PadKey = Object.freeze({
    Up: 0, "0": "Up",
    Down: 1, "1": "Down",
    Left: 2, "2": "Left",
    Right: 3, "3": "Right",
    Start: 4, "4": "Start",
    Select: 5, "5": "Select",
    A: 6, "6": "A",
    B: 7, "7": "B",
});

/**
 * Represents a palette together with the metadata
 * that is associated with it.
 */
export class PaletteInfo {
    __destroy_into_raw() {
        const ptr = this.__wbg_ptr;
        this.__wbg_ptr = 0;
        PaletteInfoFinalization.unregister(this);
        return ptr;
    }
    free() {
        const ptr = this.__destroy_into_raw();
        wasm.__wbg_paletteinfo_free(ptr, 0);
    }
}
if (Symbol.dispose) PaletteInfo.prototype[Symbol.dispose] = PaletteInfo.prototype.free;

/**
 * @enum {0 | 1 | 2 | 3}
 */
export const PpuMode = Object.freeze({
    HBlank: 0, "0": "HBlank",
    VBlank: 1, "1": "VBlank",
    OamRead: 2, "2": "OamRead",
    VramRead: 3, "3": "VramRead",
});

/**
 * @enum {0 | 1 | 2 | 3 | 4 | 5 | 6 | 7}
 */
export const RamSize = Object.freeze({
    NoRam: 0, "0": "NoRam",
    Unused: 1, "1": "Unused",
    Size8K: 2, "2": "Size8K",
    Size16K: 3, "3": "Size16K",
    Size32K: 4, "4": "Size32K",
    Size64K: 5, "5": "Size64K",
    Size128K: 6, "6": "Size128K",
    SizeUnknown: 7, "7": "SizeUnknown",
});

/**
 * @enum {0 | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 | 9 | 10}
 */
export const Region = Object.freeze({
    World: 0, "0": "World",
    Japan: 1, "1": "Japan",
    USA: 2, "2": "USA",
    Europe: 3, "3": "Europe",
    Spain: 4, "4": "Spain",
    Italy: 5, "5": "Italy",
    France: 6, "6": "France",
    Germany: 7, "7": "Germany",
    Korean: 8, "8": "Korean",
    Australia: 9, "9": "Australia",
    Unknown: 10, "10": "Unknown",
});

export class Registers {
    static __wrap(ptr) {
        const obj = Object.create(Registers.prototype);
        obj.__wbg_ptr = ptr;
        RegistersFinalization.register(obj, obj.__wbg_ptr, obj);
        return obj;
    }
    __destroy_into_raw() {
        const ptr = this.__wbg_ptr;
        this.__wbg_ptr = 0;
        RegistersFinalization.unregister(this);
        return ptr;
    }
    free() {
        const ptr = this.__destroy_into_raw();
        wasm.__wbg_registers_free(ptr, 0);
    }
    /**
     * @returns {number}
     */
    get a() {
        const ret = wasm.__wbg_get_registers_a(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {number}
     */
    get b() {
        const ret = wasm.__wbg_get_registers_b(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {number}
     */
    get c() {
        const ret = wasm.__wbg_get_registers_c(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {number}
     */
    get d() {
        const ret = wasm.__wbg_get_registers_d(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {number}
     */
    get e() {
        const ret = wasm.__wbg_get_registers_e(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {number}
     */
    get h() {
        const ret = wasm.__wbg_get_registers_h(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {number}
     */
    get l() {
        const ret = wasm.__wbg_get_registers_l(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {number}
     */
    get ly() {
        const ret = wasm.__wbg_get_registers_ly(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {number}
     */
    get lyc() {
        const ret = wasm.__wbg_get_registers_lyc(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {number}
     */
    get pc() {
        const ret = wasm.__wbg_get_registers_pc(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {number}
     */
    get scx() {
        const ret = wasm.__wbg_get_registers_scx(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {number}
     */
    get scy() {
        const ret = wasm.__wbg_get_registers_scy(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {number}
     */
    get sp() {
        const ret = wasm.__wbg_get_registers_sp(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {number}
     */
    get wx() {
        const ret = wasm.__wbg_get_registers_wx(this.__wbg_ptr);
        return ret;
    }
    /**
     * @returns {number}
     */
    get wy() {
        const ret = wasm.__wbg_get_registers_wy(this.__wbg_ptr);
        return ret;
    }
    /**
     * @param {number} arg0
     */
    set a(arg0) {
        wasm.__wbg_set_registers_a(this.__wbg_ptr, arg0);
    }
    /**
     * @param {number} arg0
     */
    set b(arg0) {
        wasm.__wbg_set_registers_b(this.__wbg_ptr, arg0);
    }
    /**
     * @param {number} arg0
     */
    set c(arg0) {
        wasm.__wbg_set_registers_c(this.__wbg_ptr, arg0);
    }
    /**
     * @param {number} arg0
     */
    set d(arg0) {
        wasm.__wbg_set_registers_d(this.__wbg_ptr, arg0);
    }
    /**
     * @param {number} arg0
     */
    set e(arg0) {
        wasm.__wbg_set_registers_e(this.__wbg_ptr, arg0);
    }
    /**
     * @param {number} arg0
     */
    set h(arg0) {
        wasm.__wbg_set_registers_h(this.__wbg_ptr, arg0);
    }
    /**
     * @param {number} arg0
     */
    set l(arg0) {
        wasm.__wbg_set_registers_l(this.__wbg_ptr, arg0);
    }
    /**
     * @param {number} arg0
     */
    set ly(arg0) {
        wasm.__wbg_set_registers_ly(this.__wbg_ptr, arg0);
    }
    /**
     * @param {number} arg0
     */
    set lyc(arg0) {
        wasm.__wbg_set_registers_lyc(this.__wbg_ptr, arg0);
    }
    /**
     * @param {number} arg0
     */
    set pc(arg0) {
        wasm.__wbg_set_registers_pc(this.__wbg_ptr, arg0);
    }
    /**
     * @param {number} arg0
     */
    set scx(arg0) {
        wasm.__wbg_set_registers_scx(this.__wbg_ptr, arg0);
    }
    /**
     * @param {number} arg0
     */
    set scy(arg0) {
        wasm.__wbg_set_registers_scy(this.__wbg_ptr, arg0);
    }
    /**
     * @param {number} arg0
     */
    set sp(arg0) {
        wasm.__wbg_set_registers_sp(this.__wbg_ptr, arg0);
    }
    /**
     * @param {number} arg0
     */
    set wx(arg0) {
        wasm.__wbg_set_registers_wx(this.__wbg_ptr, arg0);
    }
    /**
     * @param {number} arg0
     */
    set wy(arg0) {
        wasm.__wbg_set_registers_wy(this.__wbg_ptr, arg0);
    }
}
if (Symbol.dispose) Registers.prototype[Symbol.dispose] = Registers.prototype.free;

/**
 * @enum {0 | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 | 9}
 */
export const RomSize = Object.freeze({
    Size32K: 0, "0": "Size32K",
    Size64K: 1, "1": "Size64K",
    Size128K: 2, "2": "Size128K",
    Size256K: 3, "3": "Size256K",
    Size512K: 4, "4": "Size512K",
    Size1M: 5, "5": "Size1M",
    Size2M: 6, "6": "Size2M",
    Size4M: 7, "7": "Size4M",
    Size8M: 8, "8": "Size8M",
    SizeUnknown: 9, "9": "SizeUnknown",
});

/**
 * @enum {0 | 1 | 2 | 3 | 5 | 6 | 8 | 9 | 11 | 12 | 13 | 15 | 16 | 17 | 18 | 19 | 25 | 26 | 27 | 28 | 29 | 30 | 32 | 34 | 252 | 253 | 254 | 255 | 239}
 */
export const RomType = Object.freeze({
    RomOnly: 0, "0": "RomOnly",
    Mbc1: 1, "1": "Mbc1",
    Mbc1Ram: 2, "2": "Mbc1Ram",
    Mbc1RamBattery: 3, "3": "Mbc1RamBattery",
    Mbc2: 5, "5": "Mbc2",
    Mbc2Battery: 6, "6": "Mbc2Battery",
    RomRam: 8, "8": "RomRam",
    RomRamBattery: 9, "9": "RomRamBattery",
    Mmm01: 11, "11": "Mmm01",
    Mmm01Ram: 12, "12": "Mmm01Ram",
    Mmm01RamBattery: 13, "13": "Mmm01RamBattery",
    Mbc3TimerBattery: 15, "15": "Mbc3TimerBattery",
    Mbc3TimerRamBattery: 16, "16": "Mbc3TimerRamBattery",
    Mbc3: 17, "17": "Mbc3",
    Mbc3Ram: 18, "18": "Mbc3Ram",
    Mbc3RamBattery: 19, "19": "Mbc3RamBattery",
    Mbc5: 25, "25": "Mbc5",
    Mbc5Ram: 26, "26": "Mbc5Ram",
    Mbc5RamBattery: 27, "27": "Mbc5RamBattery",
    Mbc5Rumble: 28, "28": "Mbc5Rumble",
    Mbc5RumbleRam: 29, "29": "Mbc5RumbleRam",
    Mbc5RumbleRamBattery: 30, "30": "Mbc5RumbleRamBattery",
    Mbc6: 32, "32": "Mbc6",
    Mbc7SensorRumbleRamBattery: 34, "34": "Mbc7SensorRumbleRamBattery",
    PocketCamera: 252, "252": "PocketCamera",
    BandaiTama5: 253, "253": "BandaiTama5",
    HuC3: 254, "254": "HuC3",
    HuC1RamBattery: 255, "255": "HuC1RamBattery",
    Unknown: 239, "239": "Unknown",
});

/**
 * @enum {1 | 2 | 3}
 */
export const SaveStateFormat = Object.freeze({
    /**
     * Boytacean Save Compressed format (BOSC).
     *
     * This format uses the Zippy compression algorithm
     * to compress the underlying BOS contents.
     *
     * The BOSC format is the default format for the
     * save state.
     *
     * Using a compressed file format means that BESS out-of-the
     * box compatibility is not possible.
     */
    Bosc: 1, "1": "Bosc",
    /**
     * Boytacean Save format (uncompressed) (BOS).
     *
     * This format is the uncompressed version of the
     * BOSC format.
     *
     * Using an uncompressed file format means that BESS
     * out-of-the box compatibility is possible.
     */
    Bos: 2, "2": "Bos",
    /**
     * Best Effort Save State format (BESS).
     *
     * This is an abstract payload opaque format created
     * by the same people of Sameboy.
     */
    Bess: 3, "3": "Bess",
});

/**
 * @enum {0 | 3}
 */
export const SgbMode = Object.freeze({
    NoSgb: 0, "0": "NoSgb",
    SgbFunctions: 3, "3": "SgbFunctions",
});

/**
 * Represents the different formats for the state storage
 * and retrieval.
 *
 * Different formats will have different levels of detail
 * and will require different amounts of data to be
 * stored and retrieved.
 * @enum {1 | 2 | 3}
 */
export const StateFormat = Object.freeze({
    /**
     * Minimal state format, meaning that only the most basic
     * elements of the component will be stored and retrieved.
     */
    Minimal: 1, "1": "Minimal",
    /**
     * Partial state format, meaning that only the essential
     * elements of the component will be stored and retrieved.
     * All the remaining data, should inferred or computed.
     */
    Partial: 2, "2": "Partial",
    /**
     * Full state format, meaning that every single element
     * of the component will be stored and retrieved. This
     * should included redundant and calculated data.
     */
    Full: 3, "3": "Full",
});

/**
 * Top level manager structure containing the
 * entrypoint static methods for saving and loading
 * [BESS](https://github.com/LIJI32/SameBoy/blob/master/BESS.md) state
 * files and buffers for the Game Boy.
 */
export class StateManager {
    __destroy_into_raw() {
        const ptr = this.__wbg_ptr;
        this.__wbg_ptr = 0;
        StateManagerFinalization.unregister(this);
        return ptr;
    }
    free() {
        const ptr = this.__destroy_into_raw();
        wasm.__wbg_statemanager_free(ptr, 0);
    }
    /**
     * @param {Uint8Array} data
     * @returns {string}
     */
    static format_str_wa(data) {
        let deferred3_0;
        let deferred3_1;
        try {
            const ptr0 = passArray8ToWasm0(data, wasm.__wbindgen_malloc);
            const len0 = WASM_VECTOR_LEN;
            const ret = wasm.statemanager_format_str_wa(ptr0, len0);
            var ptr2 = ret[0];
            var len2 = ret[1];
            if (ret[3]) {
                ptr2 = 0; len2 = 0;
                throw takeFromExternrefTable0(ret[2]);
            }
            deferred3_0 = ptr2;
            deferred3_1 = len2;
            return getStringFromWasm0(ptr2, len2);
        } finally {
            wasm.__wbindgen_free(deferred3_0, deferred3_1, 1);
        }
    }
    /**
     * @param {Uint8Array} data
     * @returns {SaveStateFormat}
     */
    static format_wa(data) {
        const ptr0 = passArray8ToWasm0(data, wasm.__wbindgen_malloc);
        const len0 = WASM_VECTOR_LEN;
        const ret = wasm.statemanager_format_wa(ptr0, len0);
        if (ret[2]) {
            throw takeFromExternrefTable0(ret[1]);
        }
        return ret[0];
    }
    /**
     * @param {Uint8Array} data
     * @param {GameBoy} gb
     * @param {SaveStateFormat | null} [format]
     * @param {ToGbOptions | null} [options]
     */
    static load_wa(data, gb, format, options) {
        const ptr0 = passArray8ToWasm0(data, wasm.__wbindgen_malloc);
        const len0 = WASM_VECTOR_LEN;
        _assertClass(gb, GameBoy);
        let ptr1 = 0;
        if (!isLikeNone(options)) {
            _assertClass(options, ToGbOptions);
            ptr1 = options.__destroy_into_raw();
        }
        const ret = wasm.statemanager_load_wa(ptr0, len0, gb.__wbg_ptr, isLikeNone(format) ? 0 : format, ptr1);
        if (ret[1]) {
            throw takeFromExternrefTable0(ret[0]);
        }
    }
    /**
     * @param {Uint8Array} data
     * @returns {BessState}
     */
    static read_bess_wa(data) {
        const ptr0 = passArray8ToWasm0(data, wasm.__wbindgen_malloc);
        const len0 = WASM_VECTOR_LEN;
        const ret = wasm.statemanager_read_bess_wa(ptr0, len0);
        if (ret[2]) {
            throw takeFromExternrefTable0(ret[1]);
        }
        return BessState.__wrap(ret[0]);
    }
    /**
     * @param {Uint8Array} data
     * @returns {BosState}
     */
    static read_bos_auto_wa(data) {
        const ptr0 = passArray8ToWasm0(data, wasm.__wbindgen_malloc);
        const len0 = WASM_VECTOR_LEN;
        const ret = wasm.statemanager_read_bos_auto_wa(ptr0, len0);
        if (ret[2]) {
            throw takeFromExternrefTable0(ret[1]);
        }
        return BosState.__wrap(ret[0]);
    }
    /**
     * @param {Uint8Array} data
     * @returns {BosState}
     */
    static read_bos_wa(data) {
        const ptr0 = passArray8ToWasm0(data, wasm.__wbindgen_malloc);
        const len0 = WASM_VECTOR_LEN;
        const ret = wasm.statemanager_read_bos_wa(ptr0, len0);
        if (ret[2]) {
            throw takeFromExternrefTable0(ret[1]);
        }
        return BosState.__wrap(ret[0]);
    }
    /**
     * @param {Uint8Array} data
     * @returns {BoscState}
     */
    static read_bosc_wa(data) {
        const ptr0 = passArray8ToWasm0(data, wasm.__wbindgen_malloc);
        const len0 = WASM_VECTOR_LEN;
        const ret = wasm.statemanager_read_bosc_wa(ptr0, len0);
        if (ret[2]) {
            throw takeFromExternrefTable0(ret[1]);
        }
        return BoscState.__wrap(ret[0]);
    }
    /**
     * @param {GameBoy} gb
     * @param {SaveStateFormat | null} [format]
     * @param {FromGbOptions | null} [options]
     * @returns {Uint8Array}
     */
    static save_wa(gb, format, options) {
        _assertClass(gb, GameBoy);
        let ptr0 = 0;
        if (!isLikeNone(options)) {
            _assertClass(options, FromGbOptions);
            ptr0 = options.__destroy_into_raw();
        }
        const ret = wasm.statemanager_save_wa(gb.__wbg_ptr, isLikeNone(format) ? 0 : format, ptr0);
        if (ret[3]) {
            throw takeFromExternrefTable0(ret[2]);
        }
        var v2 = getArrayU8FromWasm0(ret[0], ret[1]).slice();
        wasm.__wbindgen_free(ret[0], ret[1] * 1, 1);
        return v2;
    }
    /**
     * @param {Uint8Array} data
     * @param {SaveStateFormat | null} [format]
     * @returns {Uint8Array}
     */
    static thumbnail_wa(data, format) {
        const ptr0 = passArray8ToWasm0(data, wasm.__wbindgen_malloc);
        const len0 = WASM_VECTOR_LEN;
        const ret = wasm.statemanager_thumbnail_wa(ptr0, len0, isLikeNone(format) ? 0 : format);
        if (ret[3]) {
            throw takeFromExternrefTable0(ret[2]);
        }
        var v2 = getArrayU8FromWasm0(ret[0], ret[1]).slice();
        wasm.__wbindgen_free(ret[0], ret[1] * 1, 1);
        return v2;
    }
    /**
     * @param {Uint8Array} data
     * @param {string | null} [title]
     */
    static validate_wa(data, title) {
        const ptr0 = passArray8ToWasm0(data, wasm.__wbindgen_malloc);
        const len0 = WASM_VECTOR_LEN;
        var ptr1 = isLikeNone(title) ? 0 : passStringToWasm0(title, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc);
        var len1 = WASM_VECTOR_LEN;
        const ret = wasm.statemanager_validate_wa(ptr0, len0, ptr1, len1);
        if (ret[1]) {
            throw takeFromExternrefTable0(ret[0]);
        }
    }
}
if (Symbol.dispose) StateManager.prototype[Symbol.dispose] = StateManager.prototype.free;

/**
 * Represents a tile within the Game Boy context,
 * should contain the pixel buffer of the tile.
 * The tiles are always 8x8 pixels in size.
 */
export class Tile {
    static __wrap(ptr) {
        const obj = Object.create(Tile.prototype);
        obj.__wbg_ptr = ptr;
        TileFinalization.register(obj, obj.__wbg_ptr, obj);
        return obj;
    }
    __destroy_into_raw() {
        const ptr = this.__wbg_ptr;
        this.__wbg_ptr = 0;
        TileFinalization.unregister(this);
        return ptr;
    }
    free() {
        const ptr = this.__destroy_into_raw();
        wasm.__wbg_tile_free(ptr, 0);
    }
    /**
     * @returns {Uint8Array}
     */
    buffer() {
        const ret = wasm.tile_buffer(this.__wbg_ptr);
        var v1 = getArrayU8FromWasm0(ret[0], ret[1]).slice();
        wasm.__wbindgen_free(ret[0], ret[1] * 1, 1);
        return v1;
    }
    /**
     * @param {number} x
     * @param {number} y
     * @returns {number}
     */
    get(x, y) {
        const ret = wasm.tile_get(this.__wbg_ptr, x, y);
        return ret;
    }
    /**
     * @param {number} x
     * @param {number} y
     * @param {boolean} xflip
     * @param {boolean} yflip
     * @returns {number}
     */
    get_flipped(x, y, xflip, yflip) {
        const ret = wasm.tile_get_flipped(this.__wbg_ptr, x, y, xflip, yflip);
        return ret;
    }
    /**
     * @returns {Tile}
     */
    static new() {
        const ret = wasm.tile_new();
        return Tile.__wrap(ret);
    }
    /**
     * @param {number} x
     * @param {number} y
     * @param {number} value
     */
    set(x, y, value) {
        wasm.tile_set(this.__wbg_ptr, x, y, value);
    }
}
if (Symbol.dispose) Tile.prototype[Symbol.dispose] = Tile.prototype.free;

export class TileData {
    __destroy_into_raw() {
        const ptr = this.__wbg_ptr;
        this.__wbg_ptr = 0;
        TileDataFinalization.unregister(this);
        return ptr;
    }
    free() {
        const ptr = this.__destroy_into_raw();
        wasm.__wbg_tiledata_free(ptr, 0);
    }
}
if (Symbol.dispose) TileData.prototype[Symbol.dispose] = TileData.prototype.free;

export class ToGbOptions {
    __destroy_into_raw() {
        const ptr = this.__wbg_ptr;
        this.__wbg_ptr = 0;
        ToGbOptionsFinalization.unregister(this);
        return ptr;
    }
    free() {
        const ptr = this.__destroy_into_raw();
        wasm.__wbg_togboptions_free(ptr, 0);
    }
}
if (Symbol.dispose) ToGbOptions.prototype[Symbol.dispose] = ToGbOptions.prototype.free;

/**
 * Returns the current timestamp in seconds since the UNIX epoch.
 *
 * This function has different implementations depending on whether
 * the `wasm` feature is enabled or not.
 *
 * WASM implementation is a static one using `js_sys::Date`.
 * @returns {bigint}
 */
export function timestamp() {
    const ret = wasm.timestamp();
    return BigInt.asUintN(64, ret);
}
function __wbg_get_imports() {
    const import0 = {
        __proto__: null,
        __wbg___wbindgen_string_get_b0ca35b86a603356: function(arg0, arg1) {
            const obj = arg1;
            const ret = typeof(obj) === 'string' ? obj : undefined;
            var ptr1 = isLikeNone(ret) ? 0 : passStringToWasm0(ret, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc);
            var len1 = WASM_VECTOR_LEN;
            getDataViewMemory0().setInt32(arg0 + 4 * 1, len1, true);
            getDataViewMemory0().setInt32(arg0 + 4 * 0, ptr1, true);
        },
        __wbg___wbindgen_throw_344f42d3211c4765: function(arg0, arg1) {
            throw new Error(getStringFromWasm0(arg0, arg1));
        },
        __wbg_loggerCallback_38bf5613a6679d69: function(arg0, arg1) {
            var v0 = getArrayU8FromWasm0(arg0, arg1).slice();
            wasm.__wbindgen_free(arg0, arg1 * 1, 1);
            window.loggerCallback(v0);
        },
        __wbg_now_86c0d4ba3fa605b8: function() {
            const ret = Date.now();
            return ret;
        },
        __wbg_panic_1e209e0d163f1a45: function(arg0, arg1) {
            window.panic(getStringFromWasm0(arg0, arg1));
        },
        __wbg_printerCallback_8507a423ee98ee22: function(arg0, arg1) {
            var v0 = getArrayU8FromWasm0(arg0, arg1).slice();
            wasm.__wbindgen_free(arg0, arg1 * 1, 1);
            window.printerCallback(v0);
        },
        __wbg_rumbleCallback_02bd409cabb0e2ad: function(arg0) {
            window.rumbleCallback(arg0 !== 0);
        },
        __wbg_speedCallback_3110c9e1ae69aca7: function(arg0) {
            window.speedCallback(arg0);
        },
        __wbindgen_cast_0000000000000001: function(arg0, arg1) {
            // Cast intrinsic for `Ref(String) -> Externref`.
            const ret = getStringFromWasm0(arg0, arg1);
            return ret;
        },
        __wbindgen_init_externref_table: function() {
            const table = wasm.__wbindgen_externrefs;
            const offset = table.grow(4);
            table.set(0, undefined);
            table.set(offset + 0, undefined);
            table.set(offset + 1, null);
            table.set(offset + 2, true);
            table.set(offset + 3, false);
        },
    };
    return {
        __proto__: null,
        "./boytacean_bg.js": import0,
    };
}

const BessStateFinalization = (typeof FinalizationRegistry === 'undefined')
    ? { register: () => {}, unregister: () => {} }
    : new FinalizationRegistry(ptr => wasm.__wbg_bessstate_free(ptr, 1));
const BosStateFinalization = (typeof FinalizationRegistry === 'undefined')
    ? { register: () => {}, unregister: () => {} }
    : new FinalizationRegistry(ptr => wasm.__wbg_bosstate_free(ptr, 1));
const BoscStateFinalization = (typeof FinalizationRegistry === 'undefined')
    ? { register: () => {}, unregister: () => {} }
    : new FinalizationRegistry(ptr => wasm.__wbg_boscstate_free(ptr, 1));
const CartridgeFinalization = (typeof FinalizationRegistry === 'undefined')
    ? { register: () => {}, unregister: () => {} }
    : new FinalizationRegistry(ptr => wasm.__wbg_cartridge_free(ptr, 1));
const ClockFrameFinalization = (typeof FinalizationRegistry === 'undefined')
    ? { register: () => {}, unregister: () => {} }
    : new FinalizationRegistry(ptr => wasm.__wbg_clockframe_free(ptr, 1));
const FromGbOptionsFinalization = (typeof FinalizationRegistry === 'undefined')
    ? { register: () => {}, unregister: () => {} }
    : new FinalizationRegistry(ptr => wasm.__wbg_fromgboptions_free(ptr, 1));
const GameBoyFinalization = (typeof FinalizationRegistry === 'undefined')
    ? { register: () => {}, unregister: () => {} }
    : new FinalizationRegistry(ptr => wasm.__wbg_gameboy_free(ptr, 1));
const GameBoyConfigFinalization = (typeof FinalizationRegistry === 'undefined')
    ? { register: () => {}, unregister: () => {} }
    : new FinalizationRegistry(ptr => wasm.__wbg_gameboyconfig_free(ptr, 1));
const GameGenieFinalization = (typeof FinalizationRegistry === 'undefined')
    ? { register: () => {}, unregister: () => {} }
    : new FinalizationRegistry(ptr => wasm.__wbg_gamegenie_free(ptr, 1));
const GameSharkFinalization = (typeof FinalizationRegistry === 'undefined')
    ? { register: () => {}, unregister: () => {} }
    : new FinalizationRegistry(ptr => wasm.__wbg_gameshark_free(ptr, 1));
const InfoFinalization = (typeof FinalizationRegistry === 'undefined')
    ? { register: () => {}, unregister: () => {} }
    : new FinalizationRegistry(ptr => wasm.__wbg_info_free(ptr, 1));
const ObjectDataFinalization = (typeof FinalizationRegistry === 'undefined')
    ? { register: () => {}, unregister: () => {} }
    : new FinalizationRegistry(ptr => wasm.__wbg_objectdata_free(ptr, 1));
const PaletteInfoFinalization = (typeof FinalizationRegistry === 'undefined')
    ? { register: () => {}, unregister: () => {} }
    : new FinalizationRegistry(ptr => wasm.__wbg_paletteinfo_free(ptr, 1));
const RegistersFinalization = (typeof FinalizationRegistry === 'undefined')
    ? { register: () => {}, unregister: () => {} }
    : new FinalizationRegistry(ptr => wasm.__wbg_registers_free(ptr, 1));
const StateManagerFinalization = (typeof FinalizationRegistry === 'undefined')
    ? { register: () => {}, unregister: () => {} }
    : new FinalizationRegistry(ptr => wasm.__wbg_statemanager_free(ptr, 1));
const TileFinalization = (typeof FinalizationRegistry === 'undefined')
    ? { register: () => {}, unregister: () => {} }
    : new FinalizationRegistry(ptr => wasm.__wbg_tile_free(ptr, 1));
const TileDataFinalization = (typeof FinalizationRegistry === 'undefined')
    ? { register: () => {}, unregister: () => {} }
    : new FinalizationRegistry(ptr => wasm.__wbg_tiledata_free(ptr, 1));
const ToGbOptionsFinalization = (typeof FinalizationRegistry === 'undefined')
    ? { register: () => {}, unregister: () => {} }
    : new FinalizationRegistry(ptr => wasm.__wbg_togboptions_free(ptr, 1));

function addToExternrefTable0(obj) {
    const idx = wasm.__externref_table_alloc();
    wasm.__wbindgen_externrefs.set(idx, obj);
    return idx;
}

function _assertClass(instance, klass) {
    if (!(instance instanceof klass)) {
        throw new Error(`expected instance of ${klass.name}`);
    }
}

function getArrayI16FromWasm0(ptr, len) {
    ptr = ptr >>> 0;
    return getInt16ArrayMemory0().subarray(ptr / 2, ptr / 2 + len);
}

function getArrayU16FromWasm0(ptr, len) {
    ptr = ptr >>> 0;
    return getUint16ArrayMemory0().subarray(ptr / 2, ptr / 2 + len);
}

function getArrayU8FromWasm0(ptr, len) {
    ptr = ptr >>> 0;
    return getUint8ArrayMemory0().subarray(ptr / 1, ptr / 1 + len);
}

let cachedDataViewMemory0 = null;
function getDataViewMemory0() {
    if (cachedDataViewMemory0 === null || cachedDataViewMemory0.buffer.detached === true || (cachedDataViewMemory0.buffer.detached === undefined && cachedDataViewMemory0.buffer !== wasm.memory.buffer)) {
        cachedDataViewMemory0 = new DataView(wasm.memory.buffer);
    }
    return cachedDataViewMemory0;
}

let cachedInt16ArrayMemory0 = null;
function getInt16ArrayMemory0() {
    if (cachedInt16ArrayMemory0 === null || cachedInt16ArrayMemory0.byteLength === 0) {
        cachedInt16ArrayMemory0 = new Int16Array(wasm.memory.buffer);
    }
    return cachedInt16ArrayMemory0;
}

function getStringFromWasm0(ptr, len) {
    return decodeText(ptr >>> 0, len);
}

let cachedUint16ArrayMemory0 = null;
function getUint16ArrayMemory0() {
    if (cachedUint16ArrayMemory0 === null || cachedUint16ArrayMemory0.byteLength === 0) {
        cachedUint16ArrayMemory0 = new Uint16Array(wasm.memory.buffer);
    }
    return cachedUint16ArrayMemory0;
}

let cachedUint8ArrayMemory0 = null;
function getUint8ArrayMemory0() {
    if (cachedUint8ArrayMemory0 === null || cachedUint8ArrayMemory0.byteLength === 0) {
        cachedUint8ArrayMemory0 = new Uint8Array(wasm.memory.buffer);
    }
    return cachedUint8ArrayMemory0;
}

function isLikeNone(x) {
    return x === undefined || x === null;
}

function passArray8ToWasm0(arg, malloc) {
    const ptr = malloc(arg.length * 1, 1) >>> 0;
    getUint8ArrayMemory0().set(arg, ptr / 1);
    WASM_VECTOR_LEN = arg.length;
    return ptr;
}

function passArrayJsValueToWasm0(array, malloc) {
    const ptr = malloc(array.length * 4, 4) >>> 0;
    for (let i = 0; i < array.length; i++) {
        const add = addToExternrefTable0(array[i]);
        getDataViewMemory0().setUint32(ptr + 4 * i, add, true);
    }
    WASM_VECTOR_LEN = array.length;
    return ptr;
}

function passStringToWasm0(arg, malloc, realloc) {
    if (realloc === undefined) {
        const buf = cachedTextEncoder.encode(arg);
        const ptr = malloc(buf.length, 1) >>> 0;
        getUint8ArrayMemory0().subarray(ptr, ptr + buf.length).set(buf);
        WASM_VECTOR_LEN = buf.length;
        return ptr;
    }

    let len = arg.length;
    let ptr = malloc(len, 1) >>> 0;

    const mem = getUint8ArrayMemory0();

    let offset = 0;

    for (; offset < len; offset++) {
        const code = arg.charCodeAt(offset);
        if (code > 0x7F) break;
        mem[ptr + offset] = code;
    }
    if (offset !== len) {
        if (offset !== 0) {
            arg = arg.slice(offset);
        }
        ptr = realloc(ptr, len, len = offset + arg.length * 3, 1) >>> 0;
        const view = getUint8ArrayMemory0().subarray(ptr + offset, ptr + len);
        const ret = cachedTextEncoder.encodeInto(arg, view);

        offset += ret.written;
        ptr = realloc(ptr, len, offset, 1) >>> 0;
    }

    WASM_VECTOR_LEN = offset;
    return ptr;
}

function takeFromExternrefTable0(idx) {
    const value = wasm.__wbindgen_externrefs.get(idx);
    wasm.__externref_table_dealloc(idx);
    return value;
}

let cachedTextDecoder = new TextDecoder('utf-8', { ignoreBOM: true, fatal: true });
cachedTextDecoder.decode();
const MAX_SAFARI_DECODE_BYTES = 2146435072;
let numBytesDecoded = 0;
function decodeText(ptr, len) {
    numBytesDecoded += len;
    if (numBytesDecoded >= MAX_SAFARI_DECODE_BYTES) {
        cachedTextDecoder = new TextDecoder('utf-8', { ignoreBOM: true, fatal: true });
        cachedTextDecoder.decode();
        numBytesDecoded = len;
    }
    return cachedTextDecoder.decode(getUint8ArrayMemory0().subarray(ptr, ptr + len));
}

const cachedTextEncoder = new TextEncoder();

if (!('encodeInto' in cachedTextEncoder)) {
    cachedTextEncoder.encodeInto = function (arg, view) {
        const buf = cachedTextEncoder.encode(arg);
        view.set(buf);
        return {
            read: arg.length,
            written: buf.length
        };
    };
}

let WASM_VECTOR_LEN = 0;

let wasmModule, wasmInstance, wasm;
function __wbg_finalize_init(instance, module) {
    wasmInstance = instance;
    wasm = instance.exports;
    wasmModule = module;
    cachedDataViewMemory0 = null;
    cachedInt16ArrayMemory0 = null;
    cachedUint16ArrayMemory0 = null;
    cachedUint8ArrayMemory0 = null;
    wasm.__wbindgen_start();
    return wasm;
}

async function __wbg_load(module, imports) {
    if (typeof Response === 'function' && module instanceof Response) {
        if (typeof WebAssembly.instantiateStreaming === 'function') {
            try {
                return await WebAssembly.instantiateStreaming(module, imports);
            } catch (e) {
                const validResponse = module.ok && expectedResponseType(module.type);

                if (validResponse && module.headers.get('Content-Type') !== 'application/wasm') {
                    console.warn("`WebAssembly.instantiateStreaming` failed because your server does not serve Wasm with `application/wasm` MIME type. Falling back to `WebAssembly.instantiate` which is slower. Original error:\n", e);

                } else { throw e; }
            }
        }

        const bytes = await module.arrayBuffer();
        return await WebAssembly.instantiate(bytes, imports);
    } else {
        const instance = await WebAssembly.instantiate(module, imports);

        if (instance instanceof WebAssembly.Instance) {
            return { instance, module };
        } else {
            return instance;
        }
    }

    function expectedResponseType(type) {
        switch (type) {
            case 'basic': case 'cors': case 'default': return true;
        }
        return false;
    }
}

function initSync(module) {
    if (wasm !== undefined) return wasm;


    if (module !== undefined) {
        if (Object.getPrototypeOf(module) === Object.prototype) {
            ({module} = module)
        } else {
            console.warn('using deprecated parameters for `initSync()`; pass a single object instead')
        }
    }

    const imports = __wbg_get_imports();
    if (!(module instanceof WebAssembly.Module)) {
        module = new WebAssembly.Module(module);
    }
    const instance = new WebAssembly.Instance(module, imports);
    return __wbg_finalize_init(instance, module);
}

async function __wbg_init(module_or_path) {
    if (wasm !== undefined) return wasm;


    if (module_or_path !== undefined) {
        if (Object.getPrototypeOf(module_or_path) === Object.prototype) {
            ({module_or_path} = module_or_path)
        } else {
            console.warn('using deprecated parameters for the initialization function; pass a single object instead')
        }
    }

    if (module_or_path === undefined) {
        module_or_path = new URL('boytacean_bg.wasm', import.meta.url);
    }
    const imports = __wbg_get_imports();

    if (typeof module_or_path === 'string' || (typeof Request === 'function' && module_or_path instanceof Request) || (typeof URL === 'function' && module_or_path instanceof URL)) {
        module_or_path = fetch(module_or_path);
    }

    const { instance, module } = await __wbg_load(await module_or_path, imports);

    return __wbg_finalize_init(instance, module);
}

export { initSync, __wbg_init as default };
